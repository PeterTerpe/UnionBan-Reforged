package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node     NodeConfig     `yaml:"node"`
	Database DatabaseConfig `yaml:"database"`
	API      APIConfig      `yaml:"api"`
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

type PolicyConfig struct {
	AlertScore  int `yaml:"alert_score"`
	ReviewScore int `yaml:"review_score"`
	DenyScore   int `yaml:"deny_score"`
}

type P2PConfig struct {
	Enabled        bool     `yaml:"enabled"`
	BootstrapPeers []string `yaml:"bootstrap_peers"`
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

func applyDefaults(cfg *Config) {
	if cfg.Node.DisplayName == "" {
		cfg.Node.DisplayName = "New MeshBan Node"
	}

	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/meshban.db"
	}

	if cfg.API.Listen == "" {
		cfg.API.Listen = "127.0.0.1:30000"
	}
}
