package ai

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveAndLoadLocalConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "ai.json")
	want := LocalConfig{Provider: OpenAI, Model: "gpt-5.6", APIKey: "test-key"}
	if err := SaveLocalConfig(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadLocalConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Fatalf("local config = %#v, want %#v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("AI config permissions = %#o, want owner-only", info.Mode().Perm())
		}
		directory, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if directory.Mode().Perm()&0o077 != 0 {
			t.Fatalf("AI config directory permissions = %#o, want owner-only", directory.Mode().Perm())
		}
	}
}

func TestLoadLocalConfigRejectsMissingAndUnknownFields(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ai.json")
	if _, err := LoadLocalConfig(path); err == nil || !strings.Contains(err.Error(), "ai setup") {
		t.Fatalf("missing local config error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"provider":"openai","model":"gpt-5.6","api_key":"test","extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalConfig(path); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("unknown field was accepted: %v", err)
	}
}

func TestLoadLocalConfigRejectsTrailingJSONContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ai.json")
	contents := `{"provider":"openai","model":"gpt-5.6","api_key":"test"} {"provider":"openai"}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalConfig(path); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing AI config JSON was accepted: %v", err)
	}
}

func TestDefaultConfigPathIsAnchoredBelowHome(t *testing.T) {
	home := t.TempDir()
	path, err := defaultConfigPathForHome(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "phantomguard", "ai.json")
	if path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}
	if _, err := defaultConfigPathForHome("relative-home"); err == nil {
		t.Fatal("relative AI configuration home was accepted")
	}
	actualHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err = DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(actualHome, ".config", "phantomguard", "ai.json"); path != want {
		t.Fatalf("XDG_CONFIG_HOME redirected AI configuration path to %q, want %q", path, want)
	}
}

func TestLoadLocalConfigRejectsInsecureUnixModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are not represented by os.FileMode")
	}
	path := filepath.Join(t.TempDir(), "config", "ai.json")
	if err := SaveLocalConfig(path, LocalConfig{Provider: OpenAI, Model: "gpt-5.6", APIKey: "test-key"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalConfig(path); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("world-readable AI configuration was accepted: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalConfig(path); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("world-readable AI configuration directory was accepted: %v", err)
	}
}
