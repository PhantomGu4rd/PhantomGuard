// Package extractor performs static dependency extraction without evaluating source code.
package extractor

import (
	"path/filepath"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

// Relevant reports whether PhantomGuard v1 understands a repository-relative file path.
func Relevant(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	if name == "package.json" || name == "go.mod" {
		return true
	}
	if name == "go.sum" || name == "package-lock.json" || name == "npm-shrinkwrap.json" || name == "pipfile.lock" || name == "poetry.lock" || name == "uv.lock" {
		return true
	}
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return true
	}
	switch ext {
	case ".py", ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".go":
		return true
	default:
		return false
	}
}

// ProvenanceRelevant identifies manifests and lockfiles that can provide
// evidence for a staged candidate without becoming candidates themselves.
func ProvenanceRelevant(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return true
	}
	switch name {
	case "package.json", "package-lock.json", "npm-shrinkwrap.json", "go.mod", "go.sum", "pipfile.lock", "poetry.lock", "uv.lock":
		return true
	default:
		return false
	}
}

// IndexContextRelevant identifies index metadata that can affect either
// provenance or local-package filtering during a staged-only verification.
// Its contents must be read from Git, never from the working tree.
func IndexContextRelevant(path string) bool {
	if ProvenanceRelevant(path) {
		return true
	}
	return strings.EqualFold(filepath.Base(path), "tsconfig.json")
}

// Language returns the configuration language family for path.
func Language(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx":
		return "javascript"
	case ".go":
		return "go"
	default:
		name := strings.ToLower(filepath.Base(path))
		if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
			return "python"
		}
		if name == "package.json" {
			return "javascript"
		}
		if name == "go.mod" {
			return "go"
		}
		if name == "go.sum" || name == "package-lock.json" || name == "npm-shrinkwrap.json" || name == "pipfile.lock" || name == "poetry.lock" || name == "uv.lock" {
			return ""
		}
		return ""
	}
}

// Extract dispatches to a parser appropriate for path and returns human-visible notices.
func Extract(path, content, modulePrefix string) ([]model.Finding, []string, int) {
	name := strings.ToLower(filepath.Base(path))
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		findings, unscannable := Python(path, content)
		return findings, nil, unscannable
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx":
		findings, unscannable := JavaScript(path, content)
		return findings, nil, unscannable
	case ".go":
		findings, err := GoSource(path, content, modulePrefix)
		if err != nil {
			return nil, []string{path + ": invalid Go syntax skipped (" + err.Error() + ")"}, 0
		}
		return findings, nil, 0
	}
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return Requirements(path, content), nil, 0
	}
	if name == "package.json" {
		findings, err := PackageJSON(path, content)
		if err != nil {
			return nil, []string{path + ": invalid package.json skipped (" + err.Error() + ")"}, 0
		}
		return findings, nil, 0
	}
	if name == "go.mod" {
		return GoMod(path, content), nil, 0
	}
	if name == "go.sum" || name == "package-lock.json" || name == "npm-shrinkwrap.json" || name == "pipfile.lock" || name == "poetry.lock" || name == "uv.lock" {
		return nil, nil, 0
	}
	return nil, nil, 0
}
