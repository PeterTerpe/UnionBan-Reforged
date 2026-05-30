package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// NodeRecord represents a peer node stored in the local database.
type NodeRecord struct {
	NodeID      string
	Certificate string
	PublicKey   string
	Address     string
	IP          string
	TrustLevel  string
	UpdatedAt   int64
}

// Trust levels used both by the nodes table and the kick-policy evaluation.
const (
	TrustUltimate  = "ultimate"
	TrustTrusted   = "trusted"
	TrustFriend    = "friend"
	TrustUnknown   = "unknown"
	TrustUntrusted = "untrusted"
)

var validTrustLevels = map[string]bool{
	TrustUltimate:  true,
	TrustTrusted:   true,
	TrustFriend:    true,
	TrustUnknown:   true,
	TrustUntrusted: true,
}

// ListNodes returns every known peer node ordered by trust level then node id.
func (d *Database) ListNodes(ctx context.Context) ([]NodeRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT node_id, certificate, public_key, address, ip, trust_level, updated_at
		FROM nodes
		ORDER BY
			CASE trust_level
				WHEN 'ultimate'  THEN 1
				WHEN 'trusted'   THEN 2
				WHEN 'friend'    THEN 3
				WHEN 'unknown'   THEN 4
				WHEN 'untrusted' THEN 5
				ELSE 6
			END,
			node_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []NodeRecord
	for rows.Next() {
		var n NodeRecord
		if err := rows.Scan(&n.NodeID, &n.Certificate, &n.PublicKey, &n.Address, &n.IP, &n.TrustLevel, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetNode returns a single peer node by its node ID.
func (d *Database) GetNode(ctx context.Context, nodeID string) (*NodeRecord, error) {
	nodeID = strings.TrimSpace(nodeID)

	var n NodeRecord
	err := d.db.QueryRowContext(ctx, `
		SELECT node_id, certificate, public_key, address, ip, trust_level, updated_at
		FROM nodes
		WHERE node_id = ?
	`, nodeID).Scan(&n.NodeID, &n.Certificate, &n.PublicKey, &n.Address, &n.IP, &n.TrustLevel, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UpsertNode inserts or replaces a peer node record.
func (d *Database) UpsertNode(ctx context.Context, node NodeRecord) error {
	node = normalizeNodeRecord(node)

	if err := validateNodeRecord(node); err != nil {
		return err
	}

	node.UpdatedAt = time.Now().Unix()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO nodes (node_id, certificate, public_key, address, ip, trust_level, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			certificate  = excluded.certificate,
			public_key   = excluded.public_key,
			address      = excluded.address,
			ip           = excluded.ip,
			trust_level  = excluded.trust_level,
			updated_at   = excluded.updated_at
	`,
		node.NodeID,
		node.Certificate,
		node.PublicKey,
		node.Address,
		node.IP,
		node.TrustLevel,
		node.UpdatedAt,
	)
	return err
}

// DeleteNode removes a peer node from the database by its node ID.
func (d *Database) DeleteNode(ctx context.Context, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return errors.New("node id is required")
	}

	result, err := d.db.ExecContext(ctx, `DELETE FROM nodes WHERE node_id = ?`, nodeID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListNodesByTrustLevel returns nodes matching a given trust level.
func (d *Database) ListNodesByTrustLevel(ctx context.Context, trustLevel string) ([]NodeRecord, error) {
	trustLevel = strings.TrimSpace(trustLevel)

	rows, err := d.db.QueryContext(ctx, `
		SELECT node_id, certificate, public_key, address, ip, trust_level, updated_at
		FROM nodes
		WHERE trust_level = ?
		ORDER BY node_id
	`, trustLevel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []NodeRecord
	for rows.Next() {
		var n NodeRecord
		if err := rows.Scan(&n.NodeID, &n.Certificate, &n.PublicKey, &n.Address, &n.IP, &n.TrustLevel, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// IsNodeNotFound reports whether err represents a missing-node condition.
func IsNodeNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func normalizeNodeRecord(n NodeRecord) NodeRecord {
	n.NodeID = strings.TrimSpace(n.NodeID)
	n.Certificate = strings.TrimSpace(n.Certificate)
	n.PublicKey = strings.TrimSpace(n.PublicKey)
	n.Address = strings.TrimSpace(n.Address)
	n.IP = strings.TrimSpace(n.IP)
	n.TrustLevel = strings.ToLower(strings.TrimSpace(n.TrustLevel))
	if n.TrustLevel == "" {
		n.TrustLevel = TrustUnknown
	}
	return n
}

func validateNodeRecord(n NodeRecord) error {
	if n.NodeID == "" {
		return errors.New("node id is required")
	}
	if n.Certificate == "" {
		return errors.New("node certificate is required")
	}
	if n.PublicKey == "" {
		return errors.New("node public key is required")
	}
	if !validTrustLevels[n.TrustLevel] {
		return errors.New("invalid trust level: " + n.TrustLevel)
	}
	return nil
}
