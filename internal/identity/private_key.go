package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	encryptedPrivateKeyPrefix = "enc:v1:"
	encryptedPrivateKeyType   = "meshban.encrypted_private_key.v1"

	privateKeyKDFTime    uint32 = 1
	privateKeyKDFMemory  uint32 = 64 * 1024
	privateKeyKDFThreads uint8  = 4
	privateKeySaltSize          = 16
)

type encryptedPrivateKeyBox struct {
	Type       string `json:"type"`
	KDF        string `json:"kdf"`
	AEAD       string `json:"aead"`
	Time       uint32 `json:"time"`
	Memory     uint32 `json:"memory"`
	Threads    uint8  `json:"threads"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func IsEncryptedPrivateKey(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), encryptedPrivateKeyPrefix)
}

func protectPrivateKey(privateKey ed25519.PrivateKey, options KeyOptions) (string, error) {
	if !options.EncryptPrivateKey {
		return encodeBase64(privateKey), nil
	}

	passphrase := strings.TrimSpace(options.Passphrase)
	if passphrase == "" {
		return "", errors.New("private key encryption is enabled but passphrase is empty")
	}

	salt := make([]byte, privateKeySaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		privateKeyKDFTime,
		privateKeyKDFMemory,
		privateKeyKDFThreads,
		chacha20poly1305.KeySize,
	)
	defer zeroBytes(key)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}

	additionalData := []byte("MeshBan private key v1")
	ciphertext := aead.Seal(nil, nonce, privateKey, additionalData)

	box := encryptedPrivateKeyBox{
		Type:       encryptedPrivateKeyType,
		KDF:        "argon2id",
		AEAD:       "xchacha20-poly1305",
		Time:       privateKeyKDFTime,
		Memory:     privateKeyKDFMemory,
		Threads:    privateKeyKDFThreads,
		Salt:       encodeBase64(salt),
		Nonce:      encodeBase64(nonce),
		Ciphertext: encodeBase64(ciphertext),
	}

	raw, err := json.Marshal(box)
	if err != nil {
		return "", err
	}

	return encryptedPrivateKeyPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeStoredPrivateKey(value string, options KeyOptions) (ed25519.PrivateKey, error) {
	value = strings.TrimSpace(value)

	if IsEncryptedPrivateKey(value) {
		return decryptPrivateKey(value, options)
	}

	raw, err := decodeBase64(value)
	if err != nil {
		return nil, err
	}

	if len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key size")
	}

	return ed25519.PrivateKey(raw), nil
}

func decryptPrivateKey(value string, options KeyOptions) (ed25519.PrivateKey, error) {
	passphrase := strings.TrimSpace(options.Passphrase)
	if passphrase == "" {
		return nil, errors.New("private key passphrase is required")
	}

	encodedBox := strings.TrimPrefix(strings.TrimSpace(value), encryptedPrivateKeyPrefix)

	rawBox, err := base64.RawURLEncoding.DecodeString(encodedBox)
	if err != nil {
		return nil, err
	}

	var box encryptedPrivateKeyBox
	if err := json.Unmarshal(rawBox, &box); err != nil {
		return nil, err
	}

	if box.Type != encryptedPrivateKeyType {
		return nil, errors.New("invalid encrypted private key type")
	}

	if box.KDF != "argon2id" {
		return nil, fmt.Errorf("unsupported private key KDF: %s", box.KDF)
	}

	if box.AEAD != "xchacha20-poly1305" {
		return nil, fmt.Errorf("unsupported private key AEAD: %s", box.AEAD)
	}

	salt, err := decodeBase64(box.Salt)
	if err != nil {
		return nil, err
	}

	nonce, err := decodeBase64(box.Nonce)
	if err != nil {
		return nil, err
	}

	ciphertext, err := decodeBase64(box.Ciphertext)
	if err != nil {
		return nil, err
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		box.Time,
		box.Memory,
		box.Threads,
		chacha20poly1305.KeySize,
	)
	defer zeroBytes(key)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	additionalData := []byte("MeshBan private key v1")

	plaintext, err := aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, errors.New("failed to decrypt private key")
	}

	if len(plaintext) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid decrypted private key size")
	}

	return ed25519.PrivateKey(plaintext), nil
}

func zeroBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
