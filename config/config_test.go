package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsOrg(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token: t
    org: myorg
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Instances[0].Org; got != "myorg" {
		t.Errorf("Org = %q, want %q", got, "myorg")
	}
}

func TestLoadTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_SYNC_TOKEN", "ghp_from_env")
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_env: GITHUB_SYNC_TOKEN
    org: myorg
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Instances[0].Token; got != "ghp_from_env" {
		t.Errorf("Token = %q, want %q", got, "ghp_from_env")
	}
}

func TestLoadTokenEnvUnset(t *testing.T) {
	t.Setenv("GITHUB_SYNC_MISSING", "")
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_env: GITHUB_SYNC_MISSING
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `environment variable "GITHUB_SYNC_MISSING" is not set or empty`) {
		t.Errorf("expected unset env error, got %v", err)
	}
}

func TestLoadRejectsTokenAndTokenEnv(t *testing.T) {
	t.Setenv("GITHUB_SYNC_TOKEN", "ghp_from_env")
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token: ghp_inline
    token_env: GITHUB_SYNC_TOKEN
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "set only one of token, token_env or token_cli") {
		t.Errorf("expected mutual exclusion error, got %v", err)
	}
}

func TestLoadTokenFromCLI(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_cli: echo ghp_from_cli
    org: myorg
  - alias: other
    token_cli: echo ghp_from_cli
    org: otherorg
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, inst := range cfg.Instances {
		if inst.Token != "ghp_from_cli" {
			t.Errorf("%s: Token = %q, want %q", inst.Alias, inst.Token, "ghp_from_cli")
		}
	}
}

func TestLoadTokenCLIFailure(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_cli: "false"
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `token_cli "false" failed`) {
		t.Errorf("expected command failure error, got %v", err)
	}
}

func TestLoadTokenCLIEmptyOutput(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_cli: "true"
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "returned an empty token") {
		t.Errorf("expected empty token error, got %v", err)
	}
}

func TestLoadTokenCLIDoesNotLeakOutput(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_cli: echo secret1 secret2
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must print only the token") {
		t.Fatalf("expected single-token error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret1") {
		t.Errorf("error leaks command output: %v", err)
	}
}

func TestLoadTokenCLIRejectsTokenAndCLI(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token: ghp_inline
    token_cli: gh auth token
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "set only one of token, token_env or token_cli") {
		t.Errorf("expected mutual exclusion error, got %v", err)
	}
}

func TestLoadRejectsInvalidTokenEnvName(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token_env: "not a var"
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "not a valid environment variable name") {
		t.Errorf("expected invalid name error, got %v", err)
	}
}

func TestLoadRequiresTokenOrTokenEnv(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    org: myorg
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "one of token, token_env or token_cli is required") {
		t.Errorf("expected token required error, got %v", err)
	}
}

func TestLoadRequiresOrg(t *testing.T) {
	path := writeConfig(t, `
root_dir: /tmp/repos
instances:
  - alias: github
    token: t
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "org is required") {
		t.Errorf("expected 'org is required' error, got %v", err)
	}
}
