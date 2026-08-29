package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalConfig is deliberately kept outside the repository. It contains the
// optional advisory provider selection and its credential, never scan policy.
type LocalConfig struct {
	Provider Provider `json:"provider"`
	Model    string   `json:"model"`
	APIKey   string   `json:"api_key"`
}

// DefaultConfigPath is deliberately anchored below the user's home directory.
// Unlike general-purpose application configuration, a secret-bearing advisory
// config must never follow an arbitrary environment-provided directory into a
// repository or shared location.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home for AI configuration: %w", err)
	}
	return defaultConfigPathForHome(home)
}

// defaultConfigPathForHome keeps path construction independently testable
// without making the production secret location configurable by environment.
func defaultConfigPathForHome(home string) (string, error) {
	home = strings.TrimSpace(home)
	if home == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("AI configuration requires an absolute user home directory")
	}
	return filepath.Join(home, ".config", "phantomguard", "ai.json"), nil
}

// LoadLocalConfig reads only the user-local advisory configuration. A missing
// file is an actionable setup error rather than a reason to enable AI silently.
func LoadLocalConfig(path string) (LocalConfig, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return LocalConfig{}, err
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LocalConfig{}, fmt.Errorf("AI is not configured; run phantomguard ai setup")
		}
		return LocalConfig{}, fmt.Errorf("read AI configuration: %w", err)
	}
	if err := validateSecureLocalConfig(path); err != nil {
		return LocalConfig{}, err
	}
	var cfg LocalConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return LocalConfig{}, fmt.Errorf("parse AI configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return LocalConfig{}, fmt.Errorf("parse AI configuration: trailing JSON content")
	}
	cfg.Provider = canonicalProvider(cfg.Provider)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if !isSupportedProvider(cfg.Provider) {
		return LocalConfig{}, fmt.Errorf("AI configuration has unsupported provider %q", cfg.Provider)
	}
	if cfg.APIKey == "" {
		return LocalConfig{}, fmt.Errorf("AI configuration has no API key; run phantomguard ai setup")
	}
	if _, err := resolvedModel(cfg.Provider, cfg.Model); err != nil {
		return LocalConfig{}, fmt.Errorf("AI configuration: %w", err)
	}
	return cfg, nil
}

// SaveLocalConfig writes the advisory configuration with owner-only file and
// directory modes, then atomically replaces the previous file.
func SaveLocalConfig(path string, cfg LocalConfig) error {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return err
		}
	}
	cfg.Provider = canonicalProvider(cfg.Provider)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if !isSupportedProvider(cfg.Provider) {
		return fmt.Errorf("unsupported AI provider %q", cfg.Provider)
	}
	if cfg.APIKey == "" {
		return fmt.Errorf("AI API key is required")
	}
	model, err := resolvedModel(cfg.Provider, cfg.Model)
	if err != nil {
		return err
	}
	cfg.Model = model
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create AI configuration directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("secure AI configuration directory: %w", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode AI configuration: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "ai-*.json")
	if err != nil {
		return fmt.Errorf("create AI configuration: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure AI configuration: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write AI configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close AI configuration: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace AI configuration: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure AI configuration: %w", err)
	}
	return nil
}

// Client builds the remote advisory client from local configuration only.
func (c LocalConfig) Client() (*Client, error) {
	return NewClientWithKey(c.Provider, c.Model, c.APIKey)
}
