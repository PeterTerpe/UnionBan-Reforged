package minecraft

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/PeterTerpe/MeshBan/internal/database"
)

type bannedPlayersJSONEntry struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func loadBannedPlayersFile(path string) (map[string]ServerBan, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []bannedPlayersJSONEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}

	bans := make(map[string]ServerBan, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if !isSafePlayerName(name) {
			continue
		}

		bans[strings.ToLower(name)] = ServerBan{
			Name:       name,
			UUID:       database.NormalizePlayerUUID(entry.UUID),
			Reason:     strings.TrimSpace(entry.Reason),
			UUIDSource: "official",
		}
	}

	return bans, nil
}
