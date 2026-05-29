package minecraft

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PeterTerpe/MeshBan/internal/database"
)

var (
	listUUIDPattern     = regexp.MustCompile(`([A-Za-z0-9_]{1,16})\s+\(([0-9A-Fa-f-]{32,36})\)`)
	entityUUIDPattern   = regexp.MustCompile(`\[I;\s*(-?\d+),\s*(-?\d+),\s*(-?\d+),\s*(-?\d+)\]`)
	logUUIDPattern      = regexp.MustCompile(`UUID of player ([A-Za-z0-9_]{1,16}) is ([0-9A-Fa-f-]{32,36})`)
	logJoinPattern      = regexp.MustCompile(`(?:^|:\s)([A-Za-z0-9_]{1,16}) joined the game$`)
	logLoginPattern     = regexp.MustCompile(`(?:^|:\s)([A-Za-z0-9_]{1,16})\[[^\]]+\] logged in with entity id `)
	logLeavePattern     = regexp.MustCompile(`(?:^|:\s)([A-Za-z0-9_]{1,16}) left the game$`)
	playerNamePattern   = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)
	controlCharReplacer = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")
)

type Player struct {
	Name       string
	UUID       string
	UUIDSource string
}

type ServerBan struct {
	Name       string
	UUID       string
	Reason     string
	UUIDSource string
}

func parseListUUIDs(response string) []Player {
	matches := listUUIDPattern.FindAllStringSubmatch(response, -1)
	players := make([]Player, 0, len(matches))

	for _, match := range matches {
		players = append(players, Player{
			Name:       match[1],
			UUID:       database.NormalizePlayerUUID(match[2]),
			UUIDSource: "official",
		})
	}

	return players
}

func parseListNames(response string) []Player {
	_, namesText, ok := strings.Cut(response, ":")
	if !ok {
		return nil
	}

	namesText = strings.TrimSpace(namesText)
	if namesText == "" {
		return nil
	}

	parts := strings.Split(namesText, ",")
	players := make([]Player, 0, len(parts))

	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || !isSafePlayerName(name) {
			continue
		}

		players = append(players, Player{Name: name})
	}

	return players
}

func parseBanListPlayers(response string) []ServerBan {
	_, namesText, ok := strings.Cut(response, ":")
	if !ok {
		return nil
	}

	namesText = strings.TrimSpace(namesText)
	if namesText == "" {
		return nil
	}

	parts := strings.Split(namesText, ",")
	bans := make([]ServerBan, 0, len(parts))

	for _, part := range parts {
		ban, ok := parseServerBanPart(part)
		if !ok {
			continue
		}

		bans = append(bans, ban)
	}

	return bans
}

func parseServerBanPart(value string) (ServerBan, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ServerBan{}, false
	}

	name := value
	reason := ""

	// Handle vanilla format (1.20+): "PlayerName was banned by Source: Reason"
	if idx := strings.Index(value, " was banned by "); idx >= 0 {
		name = strings.TrimSpace(value[:idx])
		if rest := value[idx+len(" was banned by "):]; rest != "" {
			// The rest is "Source: Reason"; extract reason after the first colon.
			if _, r, ok := strings.Cut(rest, ":"); ok {
				reason = strings.TrimSpace(r)
			}
		}
	} else if left, right, ok := strings.Cut(value, ":"); ok {
		name = strings.TrimSpace(left)
		reason = strings.TrimSpace(right)
	} else if strings.HasSuffix(value, ")") {
		if left, right, ok := strings.Cut(value, "("); ok {
			name = strings.TrimSpace(left)
			reason = strings.TrimSuffix(strings.TrimSpace(right), ")")
		}
	}

	if !isSafePlayerName(name) {
		return ServerBan{}, false
	}

	return ServerBan{
		Name:   name,
		Reason: reason,
	}, true
}

func parseEntityUUID(response string) (string, bool) {
	match := entityUUIDPattern.FindStringSubmatch(response)
	if len(match) != 5 {
		return "", false
	}

	raw := make([]byte, 16)

	for i := 0; i < 4; i++ {
		value, err := strconv.ParseInt(match[i+1], 10, 32)
		if err != nil {
			return "", false
		}

		binary.BigEndian.PutUint32(raw[i*4:(i+1)*4], uint32(int32(value)))
	}

	return database.NormalizePlayerUUID(fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	)), true
}

func parseLogUUID(line string) (Player, bool) {
	match := logUUIDPattern.FindStringSubmatch(line)
	if len(match) != 3 {
		return Player{}, false
	}

	if !isSafePlayerName(match[1]) {
		return Player{}, false
	}

	return Player{
		Name:       match[1],
		UUID:       database.NormalizePlayerUUID(match[2]),
		UUIDSource: "official",
	}, true
}

func parseLogJoin(line string) (string, bool) {
	for _, pattern := range []*regexp.Regexp{logJoinPattern, logLoginPattern} {
		match := pattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}

		if isSafePlayerName(match[1]) {
			return match[1], true
		}
	}

	return "", false
}

func parseLogLeave(line string) (string, bool) {
	match := logLeavePattern.FindStringSubmatch(line)
	if len(match) != 2 || !isSafePlayerName(match[1]) {
		return "", false
	}

	return match[1], true
}

func isSafePlayerName(name string) bool {
	return playerNamePattern.MatchString(name)
}

func sanitizeKickMessage(message string) string {
	message = controlCharReplacer.Replace(message)
	message = strings.Join(strings.Fields(message), " ")

	if message == "" {
		return "Kicked by MeshBan."
	}

	return message
}
