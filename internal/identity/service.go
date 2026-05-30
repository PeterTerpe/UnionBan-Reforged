package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
)

const (
	certificateType   = "meshban.node_certificate.v1"
	keyPairExportType = "meshban.key_pair_export.v1"
)

type Service struct {
	mu         sync.RWMutex
	database   *database.Database
	current    *Identity
	keyOptions KeyOptions
}

type Identity struct {
	NodeID      string
	PublicKey   string
	PrivateKey  string
	Certificate string
	CreatedAt   int64
	UpdatedAt   int64
}

type Certificate struct {
	Type        string `json:"type"`
	NodeID      string `json:"node_id"`
	PublicKey   string `json:"public_key"`
	DisplayName string `json:"display_name"`
	CreatedAt   int64  `json:"created_at"`
	Signature   string `json:"signature"`
}

type KeyPairExport struct {
	Type        string `json:"type"`
	NodeID      string `json:"node_id"`
	PublicKey   string `json:"public_key"`
	PrivateKey  string `json:"private_key"`
	Certificate string `json:"certificate"`
	ExportedAt  int64  `json:"exported_at"`
}

type KeyOptions struct {
	EncryptPrivateKey bool
	Passphrase        string
}

func LoadOrCreate(ctx context.Context, db *database.Database, displayName string, keyOptions KeyOptions) (*Service, error) {
	service := &Service{
		database:   db,
		keyOptions: keyOptions,
	}

	record, err := db.GetIdentity(ctx)
	if err == nil {
		service.current = identityFromRecord(record)

		if err := service.upgradePrivateKeyStorage(ctx); err != nil {
			return nil, err
		}

		return service, nil
	}

	if !database.IsIdentityNotFound(err) {
		return nil, err
	}

	identity, err := generateIdentity(displayName, keyOptions)
	if err != nil {
		return nil, err
	}

	if err := db.SaveIdentity(ctx, identity.toRecord()); err != nil {
		return nil, err
	}

	service.current = identity

	return service, nil
}

func (s *Service) Current() Identity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return *s.current
}

func (s *Service) ExportKeyPairJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	export := KeyPairExport{
		Type:        keyPairExportType,
		NodeID:      s.current.NodeID,
		PublicKey:   s.current.PublicKey,
		PrivateKey:  s.current.PrivateKey,
		Certificate: s.current.Certificate,
		ExportedAt:  time.Now().Unix(),
	}

	return json.MarshalIndent(export, "", "  ")
}

func (s *Service) ImportKeyPairJSON(ctx context.Context, raw []byte) error {
	var exported KeyPairExport

	if err := json.Unmarshal(raw, &exported); err != nil {
		return err
	}

	if exported.Type != keyPairExportType {
		return errors.New("invalid key pair export type")
	}

	if exported.PublicKey == "" || exported.PrivateKey == "" {
		return errors.New("public key and private key are required")
	}

	publicKeyBytes, err := decodeBase64(exported.PublicKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return errors.New("invalid public key size")
	}

	privateKey, err := decodeStoredPrivateKey(exported.PrivateKey, s.keyOptions)
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := ed25519.PublicKey(publicKeyBytes)

	derivedPublicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("failed to derive public key from private key")
	}

	if !bytes.Equal(derivedPublicKey, publicKey) {
		return errors.New("private key does not match public key")
	}

	expectedNodeID := NodeIDFromPublicKey(publicKey)
	if exported.NodeID != expectedNodeID {
		return errors.New("node_id does not match public key")
	}

	protectedPrivateKey, err := protectPrivateKey(privateKey, s.keyOptions)
	if err != nil {
		return err
	}

	now := time.Now().Unix()

	identity := &Identity{
		NodeID:     exported.NodeID,
		PublicKey:  exported.PublicKey,
		PrivateKey: protectedPrivateKey,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	certificate, err := createCertificate(identity, "Imported MeshBan Node", s.keyOptions)
	if err != nil {
		return err
	}

	identity.Certificate = certificate

	if err := s.database.SaveIdentity(ctx, identity.toRecord()); err != nil {
		return err
	}

	s.mu.Lock()
	s.current = identity
	s.mu.Unlock()

	return nil
}

func (s *Service) SignLocalBan(playerUUID string, reason string, sourceNodeID string, updatedAt int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	privateKey, err := decodeStoredPrivateKey(s.current.PrivateKey, s.keyOptions)
	if err != nil {
		return "", err
	}

	message := buildBanSignaturePayload(playerUUID, reason, sourceNodeID, updatedAt)
	signature := ed25519.Sign(privateKey, []byte(message))

	return encodeBase64(signature), nil
}

// VerifyBanSignature checks whether a ban entry's signature is valid against the local identity's public key.
// It returns nil if valid, or an error describing why verification failed.
func (s *Service) VerifyBanSignature(playerUUID, reason, sourceNodeID, signature string, updatedAt int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return VerifyBanSignatureWithPublicKey(s.current.PublicKey, playerUUID, reason, sourceNodeID, signature, updatedAt)
}

// VerifyBanSignatureWithPublicKey checks whether a ban entry's signature is
// valid against an arbitrary base64-encoded ed25519 public key.  This is used
// when verifying ban entries received from a remote node whose certificate
// (and hence public key) was previously stored in the local database.
func VerifyBanSignatureWithPublicKey(publicKeyBase64 string, playerUUID, reason, sourceNodeID, signature string, updatedAt int64) error {
	pubKeyBytes, err := decodeBase64(publicKeyBase64)
	if err != nil {
		return fmt.Errorf("failed to decode public key: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("public key has unexpected size %d", len(pubKeyBytes))
	}

	sigBytes, err := decodeBase64(signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	message := buildBanSignaturePayload(playerUUID, reason, sourceNodeID, updatedAt)

	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), sigBytes) {
		return fmt.Errorf("signature verification failed: payload does not match signature")
	}

	return nil
}

func generateIdentity(displayName string, keyOptions KeyOptions) (*Identity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()

	protectedPrivateKey, err := protectPrivateKey(privateKey, keyOptions)
	if err != nil {
		return nil, err
	}

	identity := &Identity{
		NodeID:     NodeIDFromPublicKey(publicKey),
		PublicKey:  encodeBase64(publicKey),
		PrivateKey: protectedPrivateKey,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	certificate, err := createCertificate(identity, displayName, keyOptions)
	if err != nil {
		return nil, err
	}

	identity.Certificate = certificate

	return identity, nil
}

func createCertificate(identity *Identity, displayName string, keyOptions KeyOptions) (string, error) {
	privateKey, err := decodeStoredPrivateKey(identity.PrivateKey, keyOptions)
	if err != nil {
		return "", err
	}

	cert := Certificate{
		Type:        certificateType,
		NodeID:      identity.NodeID,
		PublicKey:   identity.PublicKey,
		DisplayName: displayName,
		CreatedAt:   time.Now().Unix(),
	}

	payload := buildCertificatePayload(cert)
	signature := ed25519.Sign(privateKey, []byte(payload))
	cert.Signature = encodeBase64(signature)

	raw, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func NodeIDFromPublicKey(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))
}

func identityFromRecord(record *database.IdentityRecord) *Identity {
	return &Identity{
		NodeID:      record.NodeID,
		PublicKey:   record.PublicKey,
		PrivateKey:  record.PrivateKey,
		Certificate: record.Certificate,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

func (i *Identity) toRecord() database.IdentityRecord {
	return database.IdentityRecord{
		NodeID:      i.NodeID,
		PublicKey:   i.PublicKey,
		PrivateKey:  i.PrivateKey,
		Certificate: i.Certificate,
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
	}
}

func buildCertificatePayload(cert Certificate) string {
	parts := []string{
		"MeshBan certificate v1",
		cert.Type,
		cert.NodeID,
		cert.PublicKey,
		cert.DisplayName,
		fmt.Sprintf("%d", cert.CreatedAt),
	}

	return strings.Join(parts, "\n")
}

func buildBanSignaturePayload(playerUUID string, reason string, sourceNodeID string, updatedAt int64) string {
	parts := []string{
		"MeshBan local ban v1",
		strings.TrimSpace(playerUUID),
		strings.TrimSpace(reason),
		strings.TrimSpace(sourceNodeID),
		fmt.Sprintf("%d", updatedAt),
	}

	return strings.Join(parts, "\n")
}

func encodeBase64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeBase64(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
}

func (s *Service) UpdateKeyProtection(ctx context.Context, keyOptions KeyOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	privateKey, err := decodeStoredPrivateKey(s.current.PrivateKey, s.keyOptions)
	if err != nil {
		return err
	}

	protectedPrivateKey, err := protectPrivateKey(privateKey, keyOptions)
	if err != nil {
		return err
	}

	s.current.PrivateKey = protectedPrivateKey
	s.current.UpdatedAt = time.Now().Unix()
	s.keyOptions = keyOptions

	return s.database.SaveIdentity(ctx, s.current.toRecord())
}

func (s *Service) upgradePrivateKeyStorage(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.keyOptions.EncryptPrivateKey {
		return nil
	}

	if IsEncryptedPrivateKey(s.current.PrivateKey) {
		return nil
	}

	privateKey, err := decodeStoredPrivateKey(s.current.PrivateKey, s.keyOptions)
	if err != nil {
		return err
	}

	protectedPrivateKey, err := protectPrivateKey(privateKey, s.keyOptions)
	if err != nil {
		return err
	}

	s.current.PrivateKey = protectedPrivateKey
	s.current.UpdatedAt = time.Now().Unix()

	return s.database.SaveIdentity(ctx, s.current.toRecord())
}

func (s *Service) CreateNewIdentity(ctx context.Context, displayName string) error {
	newIdentity, err := generateIdentity(displayName, s.keyOptions)
	if err != nil {
		return err
	}

	if err := s.database.SaveIdentity(ctx, newIdentity.toRecord()); err != nil {
		return err
	}

	s.mu.Lock()
	s.current = newIdentity
	s.mu.Unlock()

	return nil
}
