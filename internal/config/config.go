package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node      NodeConfig      `yaml:"node"`
	Database  DatabaseConfig  `yaml:"database"`
	WebUI     WebUIConfig     `yaml:"webui"`
	Security  SecurityConfig  `yaml:"security"`
	Secrets   SecretsConfig   `yaml:"secrets"`
	P2P       P2PConfig       `yaml:"p2p"`
	Minecraft MinecraftConfig `yaml:"minecraft,omitempty"`
}

type NodeConfig struct {
	DisplayName string `yaml:"display_name"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
	Token  string `yaml:"token"`
}

type WebUIConfig struct {
	Listen string `yaml:"listen"`
}

type SecurityConfig struct {
	EncryptPrivateKey bool `yaml:"encrypt_private_key"`
}

type SecretsConfig struct {
	EnvFile string `yaml:"env_file"`
}

type P2PConfig struct {
	Enabled        bool     `yaml:"enabled"`
	BootstrapPeers []string `yaml:"bootstrap_peers"`
}

type MinecraftConfig struct {
	Enabled       bool                        `yaml:"enabled"`
	DefaultPolicy MinecraftPolicyConfig       `yaml:"default_policy,omitempty"`
	UUIDResolver  MinecraftUUIDResolverConfig `yaml:"uuid_resolver,omitempty"`
	Instances     []MinecraftInstanceConfig   `yaml:"instances,omitempty"`
}

type MinecraftPolicyConfig struct {
	KickMessage    string `yaml:"kick_message,omitempty"`
	KickReason     string `yaml:"kick_reason,omitempty"`
	SupportContact string `yaml:"support_contact,omitempty"`
	Ultimate       *int   `yaml:"ultimate,omitempty"`
	Trusted        *int   `yaml:"trusted,omitempty"`
	Friend         *int   `yaml:"friend,omitempty"`
	Unknown        *int   `yaml:"unknown,omitempty"`
	Untrusted      *int   `yaml:"untrusted,omitempty"`
}

type MinecraftUUIDResolverConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Endpoint          string `yaml:"endpoint,omitempty"`
	ResponseUUIDField string `yaml:"response_uuid_field,omitempty"`
	TimeoutSeconds    int    `yaml:"timeout_seconds,omitempty"`
	RetryCount        int    `yaml:"retry_count,omitempty"`
	ProxyType         string `yaml:"proxy_type,omitempty"`
	ProxyURL          string `yaml:"proxy_url,omitempty"`
	ProxyURLEnv       string `yaml:"proxy_url_env,omitempty"`
	ProxyAuth         bool   `yaml:"proxy_auth,omitempty"`
	ProxyUsernameEnv  string `yaml:"proxy_username_env,omitempty"`
	ProxyPassEnv      string `yaml:"proxy_pass_env,omitempty"`
}

type MinecraftInstanceConfig struct {
	ID                string                      `yaml:"id"`
	Enabled           bool                        `yaml:"enabled"`
	Mode              string                      `yaml:"mode"`
	RCON              MinecraftRCONConfig         `yaml:"rcon,omitempty"`
	Log               MinecraftLogConfig          `yaml:"log,omitempty"`
	BannedPlayersPath string                      `yaml:"banned_players_path,omitempty"`
	Policy            MinecraftPolicyConfig       `yaml:"policy,omitempty"`
	UUIDResolver      MinecraftUUIDResolverConfig `yaml:"uuid_resolver,omitempty"`
	PaperAdapter      string                      `yaml:"paper_adapter,omitempty"`
	AdapterTokenEnv   string                      `yaml:"adapter_token_env,omitempty"`
}

type MinecraftRCONConfig struct {
	Host                  string `yaml:"host"`
	Port                  int    `yaml:"port"`
	PasswordEnv           string `yaml:"password_env"`
	PollIntervalSeconds   int    `yaml:"poll_interval_seconds"`
	CommandTimeoutSeconds int    `yaml:"command_timeout_seconds"`
}

type MinecraftLogConfig struct {
	Path                string `yaml:"path,omitempty"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds,omitempty"`
	ReadFromEndOnStart  *bool  `yaml:"read_from_end_on_start,omitempty"`
}

func LoadOrCreate(path string, examplePath string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := createConfigFromExample(path, examplePath); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return Load(path)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	return &cfg, nil
}

func ApplyDefaults(cfg *Config) {
	applyDefaults(cfg)
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func createConfigFromExample(path string, examplePath string) error {
	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("failed to read example config %q: %w", examplePath, err)
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %q: %w", dir, err)
		}
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("failed to create config file %q: %w", path, err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write config file %q: %w", path, err)
	}

	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Node.DisplayName == "" {
		cfg.Node.DisplayName = "New MeshBan Node"
	}

	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/meshban.db"
	}

	if cfg.WebUI.Listen == "" {
		cfg.WebUI.Listen = "127.0.0.1:30000"
	}

	if cfg.Secrets.EnvFile == "" {
		cfg.Secrets.EnvFile = ".env"
	}

	if cfg.Minecraft.Enabled || len(cfg.Minecraft.Instances) > 0 {
		applyMinecraftDefaults(&cfg.Minecraft)
	}
}

func applyMinecraftDefaults(cfg *MinecraftConfig) {
	applyMinecraftPolicyDefaults(&cfg.DefaultPolicy)
	applyMinecraftUUIDResolverDefaults(&cfg.UUIDResolver)

	for i := range cfg.Instances {
		instance := &cfg.Instances[i]

		if instance.Mode == "" {
			instance.Mode = "rcon"
		}

		if instance.RCON.Host == "" {
			instance.RCON.Host = "127.0.0.1"
		}

		if instance.RCON.Port == 0 {
			instance.RCON.Port = 25575
		}

		if instance.RCON.PollIntervalSeconds <= 0 {
			instance.RCON.PollIntervalSeconds = 60
		}

		if instance.RCON.CommandTimeoutSeconds <= 0 {
			instance.RCON.CommandTimeoutSeconds = 3
		}

		if instance.Log.PollIntervalSeconds <= 0 {
			instance.Log.PollIntervalSeconds = 1
		}

		if instance.Log.ReadFromEndOnStart == nil {
			instance.Log.ReadFromEndOnStart = boolPtr(true)
		}
	}
}

func applyMinecraftPolicyDefaults(policy *MinecraftPolicyConfig) {
	if policy.KickMessage == "" {
		policy.KickMessage = "You have been kicked because this server is using MeshBan and you satisfied this server's kick policy: {policy_met}."
	}

	if policy.SupportContact == "" {
		policy.SupportContact = "server admin"
	}

	if policy.Ultimate == nil {
		policy.Ultimate = intPtr(1)
	}

	if policy.Trusted == nil {
		policy.Trusted = intPtr(2)
	}

	if policy.Friend == nil {
		policy.Friend = intPtr(5)
	}

	if policy.Unknown == nil {
		policy.Unknown = intPtr(20)
	}

	if policy.Untrusted == nil {
		policy.Untrusted = intPtr(0)
	}
}

func applyMinecraftUUIDResolverDefaults(resolver *MinecraftUUIDResolverConfig) {
	if resolver.Endpoint == "" {
		resolver.Endpoint = "https://api.minecraftservices.com/minecraft/profile/lookup/name/{name}"
	}

	if resolver.ResponseUUIDField == "" {
		resolver.ResponseUUIDField = "id"
	}

	if resolver.TimeoutSeconds <= 0 {
		resolver.TimeoutSeconds = 5
	}

	if resolver.RetryCount < 0 {
		resolver.RetryCount = 0
	}

	if resolver.ProxyType == "" {
		resolver.ProxyType = "none"
	}
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
