package config

import "testing"

func TestLoadExampleConfigIncludesMinecraftRCON(t *testing.T) {
	cfg, err := Load("../../example_config.yaml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.Minecraft.Enabled {
		t.Fatal("Minecraft config should be enabled in example_config.yaml")
	}

	if len(cfg.Minecraft.Instances) == 0 {
		t.Fatal("expected at least one Minecraft instance")
	}

	instance := cfg.Minecraft.Instances[0]
	if instance.ID != "survival" {
		t.Fatalf("first instance ID = %q, want survival", instance.ID)
	}

	if instance.Mode != "rcon" {
		t.Fatalf("first instance mode = %q, want rcon", instance.Mode)
	}

	if instance.RCON.PasswordEnv != "SURVIVAL_RCON_PASS" {
		t.Fatalf("password env = %q, want SURVIVAL_RCON_PASS", instance.RCON.PasswordEnv)
	}

	if instance.RCON.PollIntervalSeconds != nil && *instance.RCON.PollIntervalSeconds != 60 {
		t.Fatalf("RCON poll interval = %v, want nil or 60", instance.RCON.PollIntervalSeconds)
	}

	if instance.Log.Path != "/home/minecraft/survival/logs/latest.log" {
		t.Fatalf("log path = %q, want latest.log path", instance.Log.Path)
	}

	if cfg.Minecraft.UUIDResolver.ProxyURL != "PROXY_URL" && cfg.Minecraft.UUIDResolver.ProxyURLEnv != "PROXY_URL" {
		t.Fatalf("proxy URL config was not loaded from example_config.yaml")
	}

	if instance.BannedPlayers.Path != "/home/minecraft/survival/banned-players.json" {
		t.Fatalf("banned_players path = %q, want survival/banned-players.json", instance.BannedPlayers.Path)
	}

	if instance.BannedPlayers.PollIntervalSeconds == nil || *instance.BannedPlayers.PollIntervalSeconds != 10 {
		t.Fatalf("banned_players poll_interval = %v, want 10", instance.BannedPlayers.PollIntervalSeconds)
	}
}
