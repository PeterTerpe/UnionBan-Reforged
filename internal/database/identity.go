package database

import (
	"context"
	"database/sql"
	"time"
)

type IdentityRecord struct {
	NodeID      string
	PublicKey   string
	PrivateKey  string
	Certificate string
	CreatedAt   int64
	UpdatedAt   int64
}

func (d *Database) GetIdentity(ctx context.Context) (*IdentityRecord, error) {
	var record IdentityRecord

	err := d.db.QueryRowContext(ctx, `
SELECT node_id, public_key, private_key, certificate, created_at, updated_at
FROM local_identity
WHERE id = 1
`).Scan(
		&record.NodeID,
		&record.PublicKey,
		&record.PrivateKey,
		&record.Certificate,
		&record.CreatedAt,
		&record.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &record, nil
}

func (d *Database) SaveIdentity(ctx context.Context, record IdentityRecord) error {
	now := time.Now().Unix()

	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}

	record.UpdatedAt = now

	_, err := d.db.ExecContext(ctx, `
INSERT INTO local_identity (
    id,
    node_id,
    public_key,
    private_key,
    certificate,
    created_at,
    updated_at
)
VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    node_id = excluded.node_id,
    public_key = excluded.public_key,
    private_key = excluded.private_key,
    certificate = excluded.certificate,
    updated_at = excluded.updated_at
`,
		record.NodeID,
		record.PublicKey,
		record.PrivateKey,
		record.Certificate,
		record.CreatedAt,
		record.UpdatedAt,
	)

	return err
}

func IsIdentityNotFound(err error) bool {
	return err == sql.ErrNoRows
}
