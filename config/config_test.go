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
