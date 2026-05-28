package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPlayerDecisionCacheInvalidatesWhenBanlistChanges(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "meshban.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	version, err := db.BanlistCacheVersion(ctx)
	if err != nil {
		t.Fatalf("BanlistCacheVersion returned error: %v", err)
	}

	playerUUID := "069a79f444e94726a5befca90e38aaf5"
	if err := db.SavePlayerDecisionCache(ctx, PlayerDecisionCacheEntry{
		ServerID:       "survival",
		PlayerUUID:     playerUUID,
		PlayerName:     "Notch",
		Decision:       PlayerDecisionAllow,
		Reason:         "no kick policy matched",
		PolicyMet:      "none",
		BanlistVersion: version,
	}); err != nil {
		t.Fatalf("SavePlayerDecisionCache returned error: %v", err)
	}

	if _, err := db.GetPlayerDecisionCache(ctx, "survival", playerUUID, version); err != nil {
		t.Fatalf("GetPlayerDecisionCache returned error: %v", err)
	}

	if _, err := db.CreateBanEntry(ctx, BanEntry{
		PlayerUUID:   playerUUID,
		Reason:       "test",
		SourceNodeID: "local",
	}); err != nil {
		t.Fatalf("CreateBanEntry returned error: %v", err)
	}

	newVersion, err := db.BanlistCacheVersion(ctx)
	if err != nil {
		t.Fatalf("BanlistCacheVersion returned error: %v", err)
	}

	if version == newVersion {
		t.Fatal("banlist cache version did not change after banlist write")
	}

	if _, err := db.GetPlayerDecisionCache(ctx, "survival", playerUUID, newVersion); !IsPlayerDecisionCacheNotFound(err) {
		t.Fatalf("stale cache lookup error = %v, want not found", err)
	}
}

func TestListBanEntriesByPlayerUUIDMatchesHyphenVariants(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "meshban.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	if _, err := db.CreateBanEntry(ctx, BanEntry{
		PlayerUUID:   "069a79f444e94726a5befca90e38aaf5",
		PlayerName:   "Notch",
		Reason:       "test",
		SourceNodeID: "local",
		UUIDSource:   "official",
	}); err != nil {
		t.Fatalf("CreateBanEntry returned error: %v", err)
	}

	entries, err := db.ListBanEntriesByPlayerUUID(ctx, "069a79f4-44e9-4726-a5be-fca90e38aaf5")
	if err != nil {
		t.Fatalf("ListBanEntriesByPlayerUUID returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries length = %d, want 1", len(entries))
	}

	if entries[0].PlayerName != "Notch" {
		t.Fatalf("player name = %q, want Notch", entries[0].PlayerName)
	}

	if entries[0].UUIDSource != "official" {
		t.Fatalf("uuid source = %q, want official", entries[0].UUIDSource)
	}
}
