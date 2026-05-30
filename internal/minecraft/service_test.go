package minecraft

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/identity"
)

func TestEvaluateBanEntriesKicksForLocalUltimateBan(t *testing.T) {
	service := &Service{localNodeID: "local-node"}
	policy := resolvedPolicy{
		kickMessage:    "kicked: {policy_met}",
		supportContact: "admin",
		thresholds: map[string]int{
			trustUltimate:  1,
			trustTrusted:   2,
			trustFriend:    5,
			trustUnknown:   20,
			trustUntrusted: 0,
		},
	}

	decision := service.evaluateBanEntries([]database.BanEntry{
		{SourceNodeID: "local-node"},
	}, policy)

	if decision.Decision != database.PlayerDecisionKick {
		t.Fatalf("decision = %q, want kick", decision.Decision)
	}

	if decision.PolicyMet != "ultimate bans 1/1" {
		t.Fatalf("policy met = %q, want ultimate bans 1/1", decision.PolicyMet)
	}
}

func TestEvaluateBanEntriesAllowsUnknownBelowThreshold(t *testing.T) {
	service := &Service{localNodeID: "local-node"}
	policy := resolvedPolicy{
		kickMessage:    "kicked: {policy_met}",
		supportContact: "admin",
		thresholds: map[string]int{
			trustUltimate:  1,
			trustTrusted:   2,
			trustFriend:    5,
			trustUnknown:   2,
			trustUntrusted: 0,
		},
	}

	decision := service.evaluateBanEntries([]database.BanEntry{
		{SourceNodeID: "remote-node"},
	}, policy)

	if decision.Decision != database.PlayerDecisionAllow {
		t.Fatalf("decision = %q, want allow", decision.Decision)
	}
}

func TestApplyConfigUpdatesConnectorStatuses(t *testing.T) {
	service := NewService(Options{})

	service.ApplyConfig(config.MinecraftConfig{
		Enabled: true,
		Instances: []config.MinecraftInstanceConfig{
			{
				ID:      "survival",
				Enabled: false,
				Mode:    "rcon",
				RCON: config.MinecraftRCONConfig{
					Host: "127.0.0.1",
					Port: 25575,
				},
			},
			{
				ID:      "modded",
				Enabled: true,
				Mode:    "paper_adapter",
			},
		},
	})

	statuses := service.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("statuses length = %d, want 2", len(statuses))
	}

	statusByID := map[string]ConnectorStatus{}
	for _, status := range statuses {
		statusByID[status.ID] = status
	}

	if statusByID["survival"].State != "disabled" {
		t.Fatalf("survival state = %q, want disabled", statusByID["survival"].State)
	}

	if statusByID["modded"].State != "unsupported" {
		t.Fatalf("modded state = %q, want unsupported", statusByID["modded"].State)
	}
}

func TestResolveJoinedPlayerUsesLogUUIDWithoutRCON(t *testing.T) {
	service := NewService(Options{})

	player, ok := service.resolveJoinedPlayer(context.Background(), "survival", nil, nil, "Notch", map[string]Player{
		"notch": {
			Name:       "Notch",
			UUID:       "069a79f4-44e9-4726-a5be-fca90e38aaf5",
			UUIDSource: "official",
		},
	})
	if !ok {
		t.Fatal("resolveJoinedPlayer returned false")
	}

	if player.UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Fatalf("uuid = %q, want log UUID", player.UUID)
	}
}

func TestImportServerBanWritesBanlistAndCache(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "meshban.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	identityService, err := identity.LoadOrCreate(ctx, db, "Test Node", identity.KeyOptions{})
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}

	service := NewService(Options{
		Database:        db,
		IdentityService: identityService,
		LocalNodeID:     identityService.Current().NodeID,
	})

	policy := resolvedPolicy{
		kickMessage:    "kicked: {policy_met}",
		supportContact: "admin",
		thresholds: map[string]int{
			trustUltimate:  1,
			trustTrusted:   2,
			trustFriend:    5,
			trustUnknown:   20,
			trustUntrusted: 0,
		},
	}

	playerUUID := "069a79f4-44e9-4726-a5be-fca90e38aaf5"
	err = service.importServerBan(ctx, "survival", ServerBan{
		Name:   "Notch",
		Reason: "griefing",
	}, Player{
		Name:       "Notch",
		UUID:       playerUUID,
		UUIDSource: "official",
	}, policy)
	if err != nil {
		t.Fatalf("importServerBan returned error: %v", err)
	}

	entries, err := db.ListBanEntriesByPlayerUUID(ctx, playerUUID)
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

	if entries[0].Signature == "" {
		t.Fatal("signature should be set")
	}

	version, err := db.BanlistCacheVersion(ctx)
	if err != nil {
		t.Fatalf("BanlistCacheVersion returned error: %v", err)
	}

	cacheEntry, err := db.GetPlayerDecisionCache(ctx, "survival", playerUUID, version)
	if err != nil {
		t.Fatalf("GetPlayerDecisionCache returned error: %v", err)
	}

	if cacheEntry.Decision != database.PlayerDecisionKick {
		t.Fatalf("cached decision = %q, want kick", cacheEntry.Decision)
	}
}
