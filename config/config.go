package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type GitHubInstance struct {
	Alias   string `yaml:"alias"`
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
	// Orgs optionally restricts syncing to these organizations/owners.
	// When empty, all accessible organizations are synced.
	Orgs []string `yaml:"orgs"`
}

// AllowsOrg reports whether repos owned by org should be synced for this instance.
func (i GitHubInstance) AllowsOrg(org string) bool {
	if len(i.Orgs) == 0 {
		return true
	}
	for _, o := range i.Orgs {
		if strings.EqualFold(o, org) {
			return true
		}
	}
	return false
}

type Config struct {
	RootDir   string           `yaml:"root_dir"`
	Workers   int              `yaml:"workers"`
	Instances []GitHubInstance `yaml:"instances"`
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".github_sync"
	}
	return filepath.Join(home, ".github_sync")
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.RootDir == "" {
		return nil, fmt.Errorf("root_dir is required in config")
	}

	// Expand ~ in root_dir
	if len(cfg.RootDir) >= 2 && cfg.RootDir[:2] == "~/" {
		home, _ := os.UserHomeDir()
		cfg.RootDir = filepath.Join(home, cfg.RootDir[2:])
	}

	if len(cfg.Instances) == 0 {
		return nil, fmt.Errorf("at least one GitHub instance is required")
	}

	for i, inst := range cfg.Instances {
		if inst.Alias == "" {
			return nil, fmt.Errorf("instance %d: alias is required", i)
		}
		if inst.Token == "" {
			return nil, fmt.Errorf("instance %s: token is required", inst.Alias)
		}
		if inst.BaseURL == "" {
			cfg.Instances[i].BaseURL = "https://api.github.com"
		}
	}

	return &cfg, nil
}

func (c *Config) GetInstance(alias string) (*GitHubInstance, error) {
	for _, inst := range c.Instances {
		if inst.Alias == alias {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance %q not found", alias)
}
