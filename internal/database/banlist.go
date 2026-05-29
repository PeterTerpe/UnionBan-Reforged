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

// BanListFilter holds optional filters and pagination for listing ban entries.
type BanListFilter struct {
	PlayerUUID   string
	PlayerName   string
	SourceNodeID string
	UUIDSource   string
	Reason       string
	Limit        int
	Offset       int
}

// BanListResult wraps entries together with a total count for pagination.
type BanListResult struct {
	Entries    []BanEntry
	TotalCount int64
}

func (d *Database) ListBanEntries(ctx context.Context, filter BanListFilter) (BanListResult, error) {
	where := "1=1"
	args := make([]any, 0)

	if filter.PlayerUUID != "" {
		where += " AND player_uuid = ?"
		args = append(args, NormalizePlayerUUID(filter.PlayerUUID))
	}
	if filter.PlayerName != "" {
		where += " AND player_name LIKE ?"
		args = append(args, "%"+filter.PlayerName+"%")
	}
	if filter.SourceNodeID != "" {
		where += " AND source_node_id LIKE ?"
		args = append(args, "%"+filter.SourceNodeID+"%")
	}
	if filter.UUIDSource != "" {
		where += " AND uuid_source LIKE ?"
		args = append(args, "%"+filter.UUIDSource+"%")
	}
	if filter.Reason != "" {
		where += " AND reason LIKE ?"
		args = append(args, "%"+filter.Reason+"%")
	}

	var totalCount int64
	countQuery := "SELECT COUNT(*) FROM banlist WHERE " + where
	if err := d.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return BanListResult{}, err
	}

	query := "SELECT id, player_uuid, player_name, reason, source_node_id, uuid_source, signature, created_at, updated_at FROM banlist WHERE " + where + " ORDER BY updated_at DESC, id DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return BanListResult{}, err
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
			return BanListResult{}, err
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return BanListResult{}, err
	}

	return BanListResult{Entries: entries, TotalCount: totalCount}, nil
}

func (d *Database) ListBanEntriesByPlayerUUID(ctx context.Context, playerUUID string) ([]BanEntry, error) {

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, player_uuid, player_name, reason, source_node_id, uuid_source, signature, created_at, updated_at
		FROM banlist
		WHERE player_uuid = ?
		ORDER BY updated_at DESC, id DESC
		`, playerUUID)
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
