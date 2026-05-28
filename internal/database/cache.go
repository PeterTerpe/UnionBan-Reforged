package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	PlayerDecisionAllow = "allow"
	PlayerDecisionKick  = "kick"
)

type PlayerDecisionCacheEntry struct {
	ServerID       string
	PlayerUUID     string
	PlayerName     string
	Decision       string
	Reason         string
	PolicyMet      string
	BanlistVersion string
	CreatedAt      int64
	UpdatedAt      int64
}

func (d *Database) BanlistCacheVersion(ctx context.Context) (string, error) {
	var count int64
	var updatedAtSum int64
	var maxUpdatedAt int64

	err := d.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(updated_at), 0),
			COALESCE(MAX(updated_at), 0)
		FROM banlist
		`).Scan(&count, &updatedAtSum, &maxUpdatedAt)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d:%d:%d", count, updatedAtSum, maxUpdatedAt), nil
}

func (d *Database) GetPlayerDecisionCache(ctx context.Context, serverID string, playerUUID string, banlistVersion string) (*PlayerDecisionCacheEntry, error) {
	serverID = strings.TrimSpace(serverID)
	playerUUID = NormalizePlayerUUID(playerUUID)
	banlistVersion = strings.TrimSpace(banlistVersion)

	var entry PlayerDecisionCacheEntry

	err := d.db.QueryRowContext(ctx, `
		SELECT server_id, player_uuid, player_name, decision, reason, policy_met, banlist_version, created_at, updated_at
		FROM player_decision_cache
		WHERE server_id = ?
			AND player_uuid = ?
			AND banlist_version = ?
		`, serverID, playerUUID, banlistVersion).Scan(
		&entry.ServerID,
		&entry.PlayerUUID,
		&entry.PlayerName,
		&entry.Decision,
		&entry.Reason,
		&entry.PolicyMet,
		&entry.BanlistVersion,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &entry, nil
}

func (d *Database) SavePlayerDecisionCache(ctx context.Context, entry PlayerDecisionCacheEntry) error {
	normalizePlayerDecisionCacheEntry(&entry)

	if err := validatePlayerDecisionCacheEntry(entry); err != nil {
		return err
	}

	now := time.Now().Unix()
	if entry.CreatedAt == 0 {
		entry.CreatedAt = now
	}

	entry.UpdatedAt = now

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO player_decision_cache (
			server_id,
			player_uuid,
			player_name,
			decision,
			reason,
			policy_met,
			banlist_version,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, player_uuid) DO UPDATE SET
			player_name = excluded.player_name,
			decision = excluded.decision,
			reason = excluded.reason,
			policy_met = excluded.policy_met,
			banlist_version = excluded.banlist_version,
			updated_at = excluded.updated_at
		`,
		entry.ServerID,
		entry.PlayerUUID,
		entry.PlayerName,
		entry.Decision,
		entry.Reason,
		entry.PolicyMet,
		entry.BanlistVersion,
		entry.CreatedAt,
		entry.UpdatedAt,
	)

	return err
}

func IsPlayerDecisionCacheNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func normalizePlayerDecisionCacheEntry(entry *PlayerDecisionCacheEntry) {
	entry.ServerID = strings.TrimSpace(entry.ServerID)
	entry.PlayerUUID = NormalizePlayerUUID(entry.PlayerUUID)
	entry.PlayerName = strings.TrimSpace(entry.PlayerName)
	entry.Decision = strings.TrimSpace(entry.Decision)
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.PolicyMet = strings.TrimSpace(entry.PolicyMet)
	entry.BanlistVersion = strings.TrimSpace(entry.BanlistVersion)
}

func validatePlayerDecisionCacheEntry(entry PlayerDecisionCacheEntry) error {
	if entry.ServerID == "" {
		return errors.New("server id is required")
	}

	if entry.PlayerUUID == "" {
		return errors.New("player uuid is required")
	}

	if entry.Decision != PlayerDecisionAllow && entry.Decision != PlayerDecisionKick {
		return errors.New("decision must be allow or kick")
	}

	if entry.BanlistVersion == "" {
		return errors.New("banlist version is required")
	}

	return nil
}
