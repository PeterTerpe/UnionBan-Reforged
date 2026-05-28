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
	PlayerName   string
	Reason       string
	SourceNodeID string
	UUIDSource   string
	Signature    string
	CreatedAt    int64
	UpdatedAt    int64
}

func (d *Database) ListBanEntries(ctx context.Context) ([]BanEntry, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, player_uuid, player_name, reason, source_node_id, uuid_source, signature, created_at, updated_at
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
			&entry.PlayerName,
			&entry.Reason,
			&entry.SourceNodeID,
			&entry.UUIDSource,
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

func (d *Database) ListBanEntriesByPlayerUUID(ctx context.Context, playerUUID string) ([]BanEntry, error) {
	compactUUID := CompactPlayerUUID(playerUUID)

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, player_uuid, player_name, reason, source_node_id, uuid_source, signature, created_at, updated_at
		FROM banlist
		WHERE lower(replace(player_uuid, '-', '')) = ?
		ORDER BY updated_at DESC, id DESC
		`, compactUUID)
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
			&entry.PlayerName,
			&entry.Reason,
			&entry.SourceNodeID,
			&entry.UUIDSource,
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
			player_name,
			reason,
			source_node_id,
			uuid_source,
			signature,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
		entry.PlayerUUID,
		entry.PlayerName,
		entry.Reason,
		entry.SourceNodeID,
		entry.UUIDSource,
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
			player_name = ?,
			reason = ?,
			source_node_id = ?,
			uuid_source = ?,
			signature = ?,
			updated_at = ?
		WHERE id = ?
		`,
		entry.PlayerUUID,
		entry.PlayerName,
		entry.Reason,
		entry.SourceNodeID,
		entry.UUIDSource,
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
	entry.PlayerUUID = NormalizePlayerUUID(entry.PlayerUUID)
	entry.PlayerName = strings.TrimSpace(entry.PlayerName)
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.SourceNodeID = strings.TrimSpace(entry.SourceNodeID)
	entry.UUIDSource = strings.TrimSpace(entry.UUIDSource)
	entry.Signature = strings.TrimSpace(entry.Signature)

	if entry.SourceNodeID == "" {
		entry.SourceNodeID = "local"
	}

	if entry.UUIDSource == "" {
		entry.UUIDSource = "manual"
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

func NormalizePlayerUUID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	compact := CompactPlayerUUID(value)

	if len(compact) != 32 || !isHex(compact) {
		return value
	}

	return compact[0:8] + "-" +
		compact[8:12] + "-" +
		compact[12:16] + "-" +
		compact[16:20] + "-" +
		compact[20:32]
}

func CompactPlayerUUID(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func isHex(value string) bool {
	for _, char := range value {
		if (char >= '0' && char <= '9') ||
			(char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F') {
			continue
		}

		return false
	}

	return true
}
