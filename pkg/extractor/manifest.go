package extractor

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

var requirementName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*`)

// Requirements extracts registry-backed requirements, discarding options, VCS, path, and URL dependencies.
func Requirements(path, content string) []model.Finding {
	var findings []model.Finding
	for line, raw := range strings.Split(content, "\n") {
		value := strings.TrimSpace(strings.Split(strings.Split(raw, "#")[0], ";")[0])
		if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") || strings.Contains(value, "://") || strings.HasPrefix(value, "git+") {
			continue
		}
		name := requirementName.FindString(value)
		if name != "" {
			findings = append(findings, model.Finding{Name: name, Ecosystem: model.PyPI, Path: path, Line: line + 1, Snippet: contextAt(content, line+1)})
		}
	}
	return findings
}

// PackageJSON parses package.json strictly, then considers only registry dependency sections.
func PackageJSON(path, content string) ([]model.Finding, error) {
	var document struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return nil, err
	}
	sections := []map[string]string{document.Dependencies, document.DevDependencies, document.PeerDependencies, document.OptionalDependencies}
	var findings []model.Finding
	for _, section := range sections {
		for name, version := range section {
			if !isRegistryNPMVersion(version) {
				continue
			}
			line := lineContaining(content, `"`+name+`"`)
			findings = append(findings, model.Finding{Name: strings.ToLower(name), Ecosystem: model.NPM, Path: path, Line: line, Snippet: contextAt(content, line)})
		}
	}
	return findings, nil
}

func isRegistryNPMVersion(version string) bool {
	value := strings.ToLower(strings.TrimSpace(version))
	if value == "" {
		return false
	}
	for _, prefix := range []string{"file:", "link:", "workspace:", "git+", "github:", "http://", "https://", "ssh:"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	return true
}

// GoMod extracts require directives while omitting replacements and local paths.
func GoMod(path, content string) []model.Finding {
	var findings []model.Finding
	inRequireBlock := false
	for number, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.Split(raw, "//")[0])
		if line == "" {
			continue
		}
		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}
		candidate := ""
		if strings.HasPrefix(line, "require ") {
			candidate = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if inRequireBlock {
			candidate = line
		}
		fields := strings.Fields(candidate)
		if len(fields) < 2 || strings.HasPrefix(fields[0], ".") || strings.HasPrefix(fields[0], "/") {
			continue
		}
		findings = append(findings, model.Finding{Name: fields[0], Ecosystem: model.Go, Path: path, Line: number + 1, Snippet: contextAt(content, number+1)})
	}
	return findings
}

// ModulePath reads the module declaration used to identify repository-local Go imports.
func ModulePath(goMod string) string {
	for _, raw := range strings.Split(goMod, "\n") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.TrimSuffix(fields[1], "/") + "/"
		}
	}
	return ""
}

func lineContaining(content, needle string) int {
	for index, line := range strings.Split(content, "\n") {
		if strings.Contains(line, needle) {
			return index + 1
		}
	}
	return 1
}
