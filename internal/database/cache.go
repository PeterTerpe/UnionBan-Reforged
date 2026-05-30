package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
		SELECT server_id, player_uuid, player_name, decision, reason, policy_met, banlist_version, updated_at
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

	entry.UpdatedAt = time.Now().Unix()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO player_decision_cache (
			server_id,
			player_uuid,
			player_name,
			decision,
			reason,
			policy_met,
			banlist_version,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
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

// DeletePlayerDecisionCache removes the cached decision for a player on a
// specific server.  It is typically called when a new ban is imported so the
// next decidePlayer call will compute a fresh decision instead of returning a
// stale "allow".
func (d *Database) DeletePlayerDecisionCache(ctx context.Context, serverID string, playerUUID string) error {
	serverID = strings.TrimSpace(serverID)
	playerUUID = NormalizePlayerUUID(playerUUID)

	if serverID == "" || playerUUID == "" {
		return nil
	}

	_, err := d.db.ExecContext(ctx, `
		DELETE FROM player_decision_cache
		WHERE server_id = ?
			AND player_uuid = ?
		`, serverID, playerUUID)

	return err
}

// ClearServerPlayerDecisionCache removes all cached decisions for a specific
// server.  It is called when the server's policy changes so stale decisions
// are not reused.
func (d *Database) ClearServerPlayerDecisionCache(ctx context.Context, serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}

	_, err := d.db.ExecContext(ctx, `
		DELETE FROM player_decision_cache
		WHERE server_id = ?
		`, serverID)

	return err
}

// ClearAllPlayerDecisionCache removes every entry from the player decision
// cache table. It is intended for manual use via the WebUI to force all cached
// decisions to be recomputed on the next player join.
func (d *Database) ClearAllPlayerDecisionCache(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM player_decision_cache`)
	return err
}

// PolicyHashKey returns the metadata key used to store a server's policy hash.
func PolicyHashKey(serverID string) string {
	return "policy_hash:" + strings.TrimSpace(serverID)
}

// GetPolicyHash returns the stored policy hash for a server, or an empty
// string if none has been recorded.
func (d *Database) GetPolicyHash(ctx context.Context, serverID string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx, `
		SELECT value FROM metadata WHERE key = ?
		`, PolicyHashKey(serverID)).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetPolicyHash stores a new policy hash for a server.
func (d *Database) SetPolicyHash(ctx context.Context, serverID string, hash string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO metadata (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value
		`, PolicyHashKey(serverID), hash)
	return err
}

// HashPolicy computes a deterministic SHA-256 hash of the resolved policy
// fields (thresholds, kick message, support contact) so the service can detect
// when a policy has changed and prune the decision cache.
func HashPolicy(kickMessage string, supportContact string, thresholds map[string]int) string {
	h := sha256.New()
	h.Write([]byte(kickMessage))
	h.Write([]byte{0})
	h.Write([]byte(supportContact))
	h.Write([]byte{0})

	// Iterate levels in a fixed order to keep the hash deterministic.
	for _, level := range []string{"ultimate", "trusted", "friend", "unknown", "untrusted"} {
		h.Write([]byte(level))
		h.Write([]byte{0})
		h.Write([]byte(fmt.Sprintf("%d", thresholds[level])))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}
