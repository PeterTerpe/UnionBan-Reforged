package nodes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
)

// Client queries remote MeshBan nodes for banlist entries and validates
// responses using each node's certificate / public key.
type Client struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a node API client with a default HTTP transport that
// enforces reasonable timeouts.
func NewClient(logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		logger: logger,
	}
}

// QueryResult holds entries returned by a remote node together with metadata
// about which node produced them.
type QueryResult struct {
	NodeID     string
	TrustLevel string
	Entries    []database.BanEntry
	Error      error
}

// QueryAllNodes fetches banlist entries for the given player UUID from every
// known node (provided as a slice).  Each node is queried concurrently;
// results are collected until all goroutines return or ctx is cancelled.
//
// Entries whose signatures fail verification are silently dropped.  Network
// errors are attached to the corresponding QueryResult so callers can see
// which nodes were unreachable.
func (c *Client) QueryAllNodes(ctx context.Context, playerUUID string, nodes []database.NodeRecord) []QueryResult {
	if len(nodes) == 0 {
		return nil
	}

	type indexedResult struct {
		index int
		QueryResult
	}

	ch := make(chan indexedResult, len(nodes))

	for i, node := range nodes {
		i, node := i, node
		go func() {
			result := c.queryNode(ctx, playerUUID, node)
			ch <- indexedResult{index: i, QueryResult: result}
		}()
	}

	results := make([]QueryResult, len(nodes))
	for range nodes {
		select {
		case <-ctx.Done():
			// Don't block forever; return what we have so far.
			return collectNonNil(results)
		case r := <-ch:
			results[r.index] = r.QueryResult
		}
	}

	return results
}

// queryNode performs a single query against a remote node and validates the
// returned entries.
func (c *Client) queryNode(ctx context.Context, playerUUID string, node database.NodeRecord) QueryResult {
	result := QueryResult{
		NodeID:     node.NodeID,
		TrustLevel: node.TrustLevel,
	}

	baseURL := c.resolveBaseURL(node)
	if baseURL == "" {
		result.Error = fmt.Errorf("cannot resolve address for node %s", node.NodeID)
		return result
	}

	url := fmt.Sprintf("%s/api/v1/player?player_uuid=%s", baseURL, playerUUID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = err
		return result
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		result.Error = fmt.Errorf("node %s returned HTTP %d: %s", node.NodeID, resp.StatusCode, strings.TrimSpace(string(body)))
		return result
	}

	var queryResp BanlistQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		result.Error = fmt.Errorf("failed to decode response from node %s: %w", node.NodeID, err)
		return result
	}

	// Compute the node ID locally from the public key to verify that the
	// stored node_id matches.  If it doesn't, use the locally-computed
	// value so trust-level classification is always based on the
	// cryptographically-verified identity.
	pubKeyBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(node.PublicKey))
	if err != nil {
		c.logger.Warn("cannot decode public key",
			"remote_node", node.NodeID,
			"error", err,
		)
		result.Error = fmt.Errorf("cannot decode public key: %w", err)
		return result
	}
	localNodeID := identity.NodeIDFromPublicKey(pubKeyBytes)
	if localNodeID != node.NodeID {
		c.logger.Warn("stored node_id does not match public key, using locally computed node_id",
			"stored_node_id", node.NodeID,
			"computed_node_id", localNodeID,
		)
	}

	// Validate each entry's signature against the node's public key.
	var validEntries []database.BanEntry
	for _, entry := range queryResp.Entries {
		if err := identity.VerifyBanSignatureWithPublicKey(
			node.PublicKey,
			entry.PlayerUUID,
			entry.Reason,
			entry.SourceNodeID,
			entry.Signature,
			entry.UpdatedAt,
		); err != nil {
			c.logger.Warn("discarding ban entry from remote node with invalid signature",
				"remote_node", localNodeID,
				"player_uuid", entry.PlayerUUID,
				"entry_id", entry.ID,
				"error", err,
			)
			continue
		}
		// Overwrite SourceNodeID to the locally-computed node ID so local
		// trust-level classification works correctly and is always based on
		// the verified public key.
		entry.SourceNodeID = localNodeID
		validEntries = append(validEntries, entry)
	}

	result.Entries = validEntries
	return result
}

// resolveBaseURL constructs an http:// URL from the node's address or IP.
// It prefers the address field (which may be a domain) and falls back to IP.
func (c *Client) resolveBaseURL(node database.NodeRecord) string {
	host := strings.TrimSpace(node.Address)
	if host == "" {
		host = strings.TrimSpace(node.IP)
	}
	if host == "" {
		return ""
	}

	// If host already contains a scheme, use it as-is.
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}

	// Strip any port that may be part of the stored address; we only need the
	// host portion because the port (when stored) is already included in the
	// address field.
	return "http://" + host
}

// BanlistQueryResponse mirrors the response from GET /api/v1/player.
type BanlistQueryResponse struct {
	PlayerUUID string              `json:"player_uuid"`
	Entries    []database.BanEntry `json:"entries"`
	Count      int                 `json:"count"`
	Timestamp  int64               `json:"timestamp"`
}

// NodeIdentityResponse mirrors the payload returned by GET /api/v1/identity.
type NodeIdentityResponse struct {
	NodeID      string `json:"node_id"`
	DisplayName string `json:"display_name"`
	Certificate string `json:"certificate"`
	CreatedAt   int64  `json:"created_at"`
}

// FetchNodeIdentity connects to a remote MeshBan node at the given address,
// retrieves its identity certificate, and cryptographically verifies it.
// On success it returns a NodeRecord ready to be inserted into the database.
//
// The address may be a bare host, host:port, or full http:// URL.  When no
// port is included, the default MeshBan API port (30000) is assumed.
func (c *Client) FetchNodeIdentity(ctx context.Context, address string) (database.NodeRecord, error) {
	baseURL := normalizeNodeAddress(address)

	url := baseURL + "/api/v1/identity"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return database.NodeRecord{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return database.NodeRecord{}, fmt.Errorf("failed to connect to node at %s: %w", address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return database.NodeRecord{}, fmt.Errorf("node at %s returned HTTP %d: %s", address, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var identityResp NodeIdentityResponse
	if err := json.NewDecoder(resp.Body).Decode(&identityResp); err != nil {
		return database.NodeRecord{}, fmt.Errorf("failed to decode identity response from %s: %w", address, err)
	}

	if identityResp.Certificate == "" {
		return database.NodeRecord{}, fmt.Errorf("node at %s returned an empty certificate", address)
	}

	// Verify the certificate cryptographically (self-signature and NodeID).
	cert, _, err := identity.VerifyCertificateFromJSON(identityResp.Certificate)
	if err != nil {
		return database.NodeRecord{}, fmt.Errorf("certificate verification failed for node at %s: %w", address, err)
	}

	// Extract the host portion from the address for storage.
	host := extractHost(address)

	c.logger.Info("fetched and verified remote node identity",
		"node_id", cert.NodeID,
		"display_name", cert.DisplayName,
		"address", host,
	)

	return database.NodeRecord{
		NodeID:      cert.NodeID,
		Certificate: identityResp.Certificate,
		PublicKey:   cert.PublicKey,
		Address:     host,
		TrustLevel:  database.TrustUnknown,
	}, nil
}

func normalizeNodeAddress(address string) string {
	address = strings.TrimSpace(address)

	hasScheme := strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://")

	var scheme, hostPort string
	if hasScheme {
		if idx := strings.Index(address, "://"); idx != -1 {
			scheme = address[:idx+3]
			hostPort = address[idx+3:]
		}
	} else {
		scheme = "http://"
		hostPort = address
	}

	// Check if a port is already included.
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil || port == "" {
		// No port – use the default.
		if host == "" {
			host = hostPort
		}
		return scheme + net.JoinHostPort(host, "30000")
	}

	return scheme + net.JoinHostPort(host, port)
}

// extractHost returns the host (and port if non-default) from a user-supplied address.
func extractHost(address string) string {
	address = strings.TrimSpace(address)

	// Strip scheme.
	if idx := strings.Index(address, "://"); idx != -1 {
		address = address[idx+3:]
	}

	return address
}

func collectNonNil(results []QueryResult) []QueryResult {
	var out []QueryResult
	for _, r := range results {
		if r.NodeID != "" {
			out = append(out, r)
		}
	}
	return out
}
