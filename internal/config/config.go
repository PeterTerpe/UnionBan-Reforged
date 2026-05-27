package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node     NodeConfig     `yaml:"node"`
	Database DatabaseConfig `yaml:"database"`
	WebUI    WebUIConfig    `yaml:"webui"`
	Security SecurityConfig `yaml:"security"`
	Secrets  SecretsConfig  `yaml:"secrets"`
	P2P      P2PConfig      `yaml:"p2p"`
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
	Listen                string `yaml:"listen"`
	RequireTokenForRemote bool   `yaml:"require_token_for_remote"`
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

func LoadOrCreate(path string, examplePath string) (*Config, error) {
	// Create the config file from the example file if it does not exist.
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
	// Read the configuration file from disk.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config

	// Decode YAML content into the Config struct.
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Fill default values when fields are not set.
	applyDefaults(&cfg)

	return &cfg, nil
}

func createConfigFromExample(path string, examplePath string) error {
	// Read the example configuration file.
	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("failed to read example config %q: %w", examplePath, err)
	}

	// Create the parent directory for the target config file if needed.
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %q: %w", dir, err)
		}
	}

	// Write the new config file without overwriting an existing file.
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
	if cfg.Secrets.EnvFile == "" {
		cfg.Secrets.EnvFile = ".env"
	}
	if cfg.WebUI.Listen == "" {
		cfg.WebUI.Listen = "127.0.0.1:30000"
	}
}
