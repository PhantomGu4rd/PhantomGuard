// Package config loads the repository-local PhantomGuard configuration.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/gitio"
)

// Config is intentionally small and JSON-only so the release binary has no parser dependency.
type Config struct {
	FailMode         string            `json:"fail_mode"`
	Languages        []string          `json:"languages"`
	Ignore           []string          `json:"ignore"`
	Allow            []string          `json:"allow"`
	Aliases          map[string]string `json:"aliases"`
	PositiveTTLHours int               `json:"cache_positive_ttl_hours"`
	NegativeTTLHours int               `json:"cache_negative_ttl_hours"`
}

// Default returns the secure, documented configuration baseline.
func Default() Config {
	return Config{
		FailMode:         "warn",
		Languages:        []string{"python", "javascript", "go"},
		Aliases:          map[string]string{},
		PositiveTTLHours: 168,
		NegativeTTLHours: 1,
	}
}

// Load reads .phantomguard.json from root. A missing file is not an error.
func Load(root string, forceStrict bool) (Config, error) {
	path := filepath.Join(root, ".phantomguard.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultWithStrict(forceStrict), nil
		}
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	return decode(raw, path, forceStrict)
}

// LoadIndex reads repository policy from the exact Git index. Staged
// verification uses it so an unstaged .phantomguard.json cannot alter hook
// behavior or exempt a dependency from inspection.
func LoadIndex(root string, forceStrict bool) (Config, error) {
	paths, err := gitio.IndexFiles(root)
	if err != nil {
		return Config{}, fmt.Errorf("list index configuration: %w", err)
	}
	for _, path := range paths {
		if filepath.ToSlash(path) != ".phantomguard.json" {
			continue
		}
		raw, err := gitio.StagedContent(root, path)
		if err != nil {
			return Config{}, fmt.Errorf("read staged %s: %w", path, err)
		}
		return decode(raw, path, forceStrict)
	}
	return defaultWithStrict(forceStrict), nil
}

func defaultWithStrict(forceStrict bool) Config {
	result := Default()
	if forceStrict || os.Getenv("PHANTOMGUARD_STRICT") == "1" {
		result.FailMode = "strict"
	}
	return result
}

func decode(raw []byte, path string, forceStrict bool) (Config, error) {
	result := Default()
	var supplied struct {
		FailMode         *string           `json:"fail_mode"`
		Languages        []string          `json:"languages"`
		Ignore           []string          `json:"ignore"`
		Allow            []string          `json:"allow"`
		Aliases          map[string]string `json:"aliases"`
		PositiveTTLHours *int              `json:"cache_positive_ttl_hours"`
		NegativeTTLHours *int              `json:"cache_negative_ttl_hours"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&supplied); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, fmt.Errorf("parse %s: trailing JSON content", path)
	}
	if supplied.FailMode != nil {
		result.FailMode = strings.ToLower(strings.TrimSpace(*supplied.FailMode))
	}
	if len(supplied.Languages) > 0 {
		result.Languages = normaliseStrings(supplied.Languages)
	}
	result.Ignore = supplied.Ignore
	result.Allow = normaliseStrings(supplied.Allow)
	if supplied.Aliases != nil {
		result.Aliases = supplied.Aliases
	}
	if supplied.PositiveTTLHours != nil {
		result.PositiveTTLHours = *supplied.PositiveTTLHours
	}
	if supplied.NegativeTTLHours != nil {
		result.NegativeTTLHours = *supplied.NegativeTTLHours
	}
	if forceStrict || os.Getenv("PHANTOMGUARD_STRICT") == "1" {
		result.FailMode = "strict"
	}
	if result.FailMode != "warn" && result.FailMode != "strict" {
		return Config{}, fmt.Errorf("fail_mode must be warn or strict")
	}
	if result.PositiveTTLHours < 1 || result.NegativeTTLHours < 1 {
		return Config{}, fmt.Errorf("cache TTL values must be at least one hour")
	}
	return result, nil
}

func normaliseStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// LanguageEnabled reports whether a language family is active in this repository.
func (c Config) LanguageEnabled(language string) bool {
	for _, allowed := range c.Languages {
		if allowed == language {
			return true
		}
	}
	return false
}
