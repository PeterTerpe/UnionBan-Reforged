package minecraft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBannedPlayersFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "banned-players.json")
	if err := os.WriteFile(path, []byte(`[
		{
			"uuid": "069a79f444e94726a5befca90e38aaf5",
			"name": "Notch",
			"created": "2026-05-28 12:00:00 +0000",
			"source": "Server",
			"expires": "forever",
			"reason": "griefing"
		},
		{
			"uuid": "853c80ef-3c37-49fd-aa49-938b674adae6",
			"name": "bad name",
			"reason": "ignored"
		}
	]`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	bans, err := loadBannedPlayersFile(path)
	if err != nil {
		t.Fatalf("loadBannedPlayersFile returned error: %v", err)
	}

	ban, ok := bans["notch"]
	if !ok {
		t.Fatal("notch ban was not loaded")
	}

	if ban.Name != "Notch" {
		t.Fatalf("name = %q, want Notch", ban.Name)
	}

	if ban.UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Fatalf("uuid = %q, want normalized UUID", ban.UUID)
	}

	if ban.Reason != "griefing" {
		t.Fatalf("reason = %q, want griefing", ban.Reason)
	}

	if ban.UUIDSource != "official" {
		t.Fatalf("uuid source = %q, want official", ban.UUIDSource)
	}

	if _, ok := bans["bad name"]; ok {
		t.Fatal("unsafe player name should not be loaded")
	}
}

func TestLoadBannedPlayersFileEmptyPath(t *testing.T) {
	bans, err := loadBannedPlayersFile(" ")
	if err != nil {
		t.Fatalf("loadBannedPlayersFile returned error: %v", err)
	}

	if bans != nil {
		t.Fatalf("bans = %#v, want nil", bans)
	}
}
