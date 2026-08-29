package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAcceptsDeterministicConfiguration(t *testing.T) {
	root := t.TempDir()
	contents := []byte(`{
  "fail_mode": "strict",
  "languages": ["python", "javascript"],
  "ignore": ["vendor/**"],
  "allow": ["company-internal"],
  "aliases": {"yaml": "PyYAML"},
  "cache_positive_ttl_hours": 48,
  "cache_negative_ttl_hours": 2
}`)
	if err := os.WriteFile(filepath.Join(root, ".phantomguard.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailMode != "strict" || len(cfg.Languages) != 2 || cfg.PositiveTTLHours != 48 || cfg.NegativeTTLHours != 2 {
		t.Fatalf("unexpected deterministic config: %#v", cfg)
	}
}

func TestLoadRejectsLegacyAIConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".phantomguard.json"), []byte(`{"ai_provider":"openai"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root, false)
	if err == nil || !strings.Contains(err.Error(), "ai_provider") {
		t.Fatalf("legacy AI repository configuration was accepted: %v", err)
	}
}

func TestLoadRejectsTrailingJSONContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".phantomguard.json"), []byte(`{"fail_mode":"warn"} {"fail_mode":"strict"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, false); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing policy JSON was accepted: %v", err)
	}
}

func TestLoadForceStrictOverridesRepositoryWarnMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".phantomguard.json"), []byte(`{"fail_mode":"warn"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailMode != "strict" {
		t.Fatalf("force strict mode = %q, want strict", cfg.FailMode)
	}
}

func TestLoadIndexIgnoresUnstagedConfiguration(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	path := filepath.Join(root, ".phantomguard.json")
	if err := os.WriteFile(path, []byte(`{"allow":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("git", "add", ".phantomguard.json")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}
	if err := os.WriteFile(path, []byte(`{"allow":["phantom-package"],"languages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadIndex(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailMode != "strict" || len(cfg.Allow) != 0 || len(cfg.Languages) == 0 {
		t.Fatalf("staged configuration was replaced by working-tree policy: %#v", cfg)
	}
}
