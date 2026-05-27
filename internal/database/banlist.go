package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type BanEntry struct {
	ID           int64
	PlayerUUID   string
	Reason       string
	SourceNodeID string
	Signature    string
	CreatedAt    int64
	UpdatedAt    int64
}

func (d *Database) ListBanEntries(ctx context.Context) ([]BanEntry, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, player_uuid, reason, source_node_id, signature, created_at, updated_at
		FROM banlist
		ORDER BY updated_at DESC, id DESC
		`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []BanEntry

	for rows.Next() {
		var entry BanEntry

		if err := rows.Scan(
			&entry.ID,
			&entry.PlayerUUID,
			&entry.Reason,
			&entry.SourceNodeID,
			&entry.Signature,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (d *Database) CreateBanEntry(ctx context.Context, entry BanEntry) (int64, error) {
	normalizeBanEntry(&entry)

	if err := validateBanEntry(entry); err != nil {
		return 0, err
	}

	now := time.Now().Unix()

	if entry.CreatedAt == 0 {
		entry.CreatedAt = now
	}

	if entry.UpdatedAt == 0 {
		entry.UpdatedAt = now
	}

	result, err := d.db.ExecContext(ctx, `
		INSERT INTO banlist (
			player_uuid,
			reason,
			source_node_id,
			signature,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		`,
		entry.PlayerUUID,
		entry.Reason,
		entry.SourceNodeID,
		entry.Signature,
		entry.CreatedAt,
		entry.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

func (d *Database) UpdateBanEntry(ctx context.Context, entry BanEntry) error {
	normalizeBanEntry(&entry)

	if entry.ID <= 0 {
		return errors.New("ban entry id is required")
	}

	if err := validateBanEntry(entry); err != nil {
		return err
	}

	if entry.UpdatedAt == 0 {
		entry.UpdatedAt = time.Now().Unix()
	}

	result, err := d.db.ExecContext(ctx, `
		UPDATE banlist
		SET player_uuid = ?,
			reason = ?,
			source_node_id = ?,
			signature = ?,
			updated_at = ?
		WHERE id = ?
		`,
		entry.PlayerUUID,
		entry.Reason,
		entry.SourceNodeID,
		entry.Signature,
		entry.UpdatedAt,
		entry.ID,
	)
	if err != nil {
		return err
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affectedRows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *Database) DeleteBanEntry(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("ban entry id is required")
	}

	result, err := d.db.ExecContext(ctx, `
		DELETE FROM banlist
		WHERE id = ?
		`, id)
	if err != nil {
		return err
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affectedRows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func normalizeBanEntry(entry *BanEntry) {
	entry.PlayerUUID = strings.TrimSpace(entry.PlayerUUID)
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.SourceNodeID = strings.TrimSpace(entry.SourceNodeID)
	entry.Signature = strings.TrimSpace(entry.Signature)

	if entry.SourceNodeID == "" {
		entry.SourceNodeID = "local"
	}
}

func validateBanEntry(entry BanEntry) error {
	if entry.PlayerUUID == "" {
		return errors.New("player uuid is required")
	}

	if entry.Reason == "" {
		return errors.New("ban reason is required")
	}

	if entry.SourceNodeID == "" {
		return errors.New("source node id is required")
	}

	return nil
}
