package minecraft

import "testing"

func TestParseListUUIDs(t *testing.T) {
	players := parseListUUIDs("There are 2 of a max of 20 players online: Notch (069a79f4-44e9-4726-a5be-fca90e38aaf5), jeb_ (853c80ef-3c37-49fd-aa49-938b674adae6)")

	if len(players) != 2 {
		t.Fatalf("players length = %d, want 2", len(players))
	}

	if players[0].Name != "Notch" || players[0].UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Fatalf("first player = %#v", players[0])
	}
}

func TestParseListNames(t *testing.T) {
	players := parseListNames("There are 2 of a max of 20 players online: Notch, jeb_")

	if len(players) != 2 {
		t.Fatalf("players length = %d, want 2", len(players))
	}

	if players[1].Name != "jeb_" {
		t.Fatalf("second player name = %q, want jeb_", players[1].Name)
	}
}

func TestParseEntityUUID(t *testing.T) {
	uuid, ok := parseEntityUUID("Notch has the following entity data: [I; 0, 0, 0, 1]")
	if !ok {
		t.Fatal("parseEntityUUID returned false")
	}

	if uuid != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("uuid = %q, want 00000000-0000-0000-0000-000000000001", uuid)
	}
}

func TestParseBanListPlayers(t *testing.T) {
	bans := parseBanListPlayers("There are 2 bans: Notch, jeb_: griefing")

	if len(bans) != 2 {
		t.Fatalf("bans length = %d, want 2", len(bans))
	}

	if bans[0].Name != "Notch" {
		t.Fatalf("first ban name = %q, want Notch", bans[0].Name)
	}

	if bans[1].Name != "jeb_" || bans[1].Reason != "griefing" {
		t.Fatalf("second ban = %#v, want jeb_ with reason griefing", bans[1])
	}
}

func TestParseLogUUID(t *testing.T) {
	player, ok := parseLogUUID("[12:00:00 INFO]: UUID of player Notch is 069a79f444e94726a5befca90e38aaf5")
	if !ok {
		t.Fatal("parseLogUUID returned false")
	}

	if player.Name != "Notch" {
		t.Fatalf("name = %q, want Notch", player.Name)
	}

	if player.UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Fatalf("uuid = %q, want normalized UUID", player.UUID)
	}

	if player.UUIDSource != "official" {
		t.Fatalf("uuid source = %q, want official", player.UUIDSource)
	}
}

func TestParseLogJoin(t *testing.T) {
	tests := []string{
		"[12:00:01 INFO]: Notch joined the game",
		"[12:00:01 INFO]: Notch[/127.0.0.1:43210] logged in with entity id 42 at ([world] 0.0, 64.0, 0.0)",
	}

	for _, test := range tests {
		name, ok := parseLogJoin(test)
		if !ok {
			t.Fatalf("parseLogJoin(%q) returned false", test)
		}

		if name != "Notch" {
			t.Fatalf("name = %q, want Notch", name)
		}
	}
}

func TestParseLogLeave(t *testing.T) {
	name, ok := parseLogLeave("[12:00:02 INFO]: Notch left the game")
	if !ok {
		t.Fatal("parseLogLeave returned false")
	}

	if name != "Notch" {
		t.Fatalf("name = %q, want Notch", name)
	}
}
