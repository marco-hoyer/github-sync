package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const tokenCommandTimeout = 30 * time.Second

type GitHubInstance struct {
	Alias   string `yaml:"alias"`
	BaseURL string `yaml:"base_url"`
	// Set only one of Token, TokenEnv or TokenCLI.
	Token   string `yaml:"token"`
	TokenEnv string `yaml:"token_env"`
	TokenCLI string `yaml:"token_cli"`
	// Org is the GitHub organization whose repos are synced into the alias dir.
	Org string `yaml:"org"`
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

	cliCache := map[string]string{}
	for i, inst := range cfg.Instances {
		if inst.Alias == "" {
			return nil, fmt.Errorf("instance %d: alias is required", i)
		}
		token, err := resolveToken(inst, cliCache)
		if err != nil {
			return nil, fmt.Errorf("instance %s: %w", inst.Alias, err)
		}
		cfg.Instances[i].Token = token
		if inst.Org == "" {
			return nil, fmt.Errorf("instance %s: org is required", inst.Alias)
		}
		if inst.BaseURL == "" {
			cfg.Instances[i].BaseURL = "https://api.github.com"
		}
	}

	return &cfg, nil
}

func resolveToken(inst GitHubInstance, cliCache map[string]string) (string, error) {
	set := 0
	for _, v := range []string{inst.Token, inst.TokenEnv, inst.TokenCLI} {
		if v != "" {
			set++
		}
	}
	switch {
	case set > 1:
		return "", fmt.Errorf("set only one of token, token_env or token_cli")
	case inst.TokenCLI != "":
		if token, ok := cliCache[inst.TokenCLI]; ok {
			return token, nil
		}
		token, err := runTokenCommand(inst.TokenCLI)
		if err != nil {
			return "", err
		}
		cliCache[inst.TokenCLI] = token
		return token, nil
	case inst.TokenEnv != "":
		if !validEnvName(inst.TokenEnv) {
			return "", fmt.Errorf("token_env %q is not a valid environment variable name", inst.TokenEnv)
		}
		token := strings.TrimSpace(os.Getenv(inst.TokenEnv))
		if token == "" {
			return "", fmt.Errorf("environment variable %q is not set or empty", inst.TokenEnv)
		}
		return token, nil
	case inst.Token != "":
		return inst.Token, nil
	default:
		return "", fmt.Errorf("one of token, token_env or token_cli is required")
	}
}

// runs command without a shell, so pipes, quotes and variable expansion are not supported
func runTokenCommand(command string) (string, error) {
	args := strings.Fields(command)
	if len(args) == 0 {
		return "", fmt.Errorf("token_cli is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), tokenCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("token_cli %q timed out after %s", args[0], tokenCommandTimeout)
		}
		return "", fmt.Errorf("token_cli %q failed: %w", args[0], err)
	}

	token := strings.TrimSpace(stdout.String())
	if token == "" {
		return "", fmt.Errorf("token_cli %q returned an empty token", args[0])
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("token_cli %q must print only the token", args[0])
	}
	return token, nil
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

func (c *Config) GetInstance(alias string) (*GitHubInstance, error) {
	for _, inst := range c.Instances {
		if inst.Alias == alias {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance %q not found", alias)
}
