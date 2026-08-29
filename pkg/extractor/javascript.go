package extractor

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/phantomguard/phantomguard/data"
	"github.com/phantomguard/phantomguard/pkg/model"
)

var (
	jsSideEffect = regexp.MustCompile(`(?m)^[\t ]*import[\t ]*["']([^"'\r\n]+)["']`)
	jsFrom       = regexp.MustCompile(`(?m)^[\t ]*(?:import|export)\b[^\r\n;]*?\bfrom[\t ]*["']([^"'\r\n]+)["']`)
	jsCall       = regexp.MustCompile(`(?m)(?:^|[;\n])[\t ]*(?:(?:const|let|var)[\t ]+[A-Za-z_$][\w$]*[\t ]*=[\t ]*)?(?:require|import)[\t ]*\([\t ]*["']([^"'\r\n]+)["']`)
	builtinOnce  sync.Once
	builtins     map[string]bool
)

// JavaScript extracts literal module specifiers after stripping JavaScript comments.
func JavaScript(path, content string) ([]model.Finding, int) {
	stripped := stripJSComments(content)
	var findings []model.Finding
	seen := make(map[string]bool)
	addMatches := func(matches [][]int) {
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}
			name := NormalizeSpecifier(stripped[match[2]:match[3]])
			if name == "" {
				continue
			}
			line := lineForOffset(stripped, match[2])
			key := name + "@" + string(rune(line))
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, model.Finding{Name: name, Ecosystem: model.NPM, Path: path, Line: line, Snippet: contextAt(content, line)})
		}
	}
	addMatches(jsSideEffect.FindAllStringSubmatchIndex(stripped, -1))
	addMatches(jsFrom.FindAllStringSubmatchIndex(stripped, -1))
	addMatches(jsCall.FindAllStringSubmatchIndex(stripped, -1))
	return findings, 0
}

// NormalizeSpecifier returns the npm root package or an empty string for local and built-in imports.
func NormalizeSpecifier(specifier string) string {
	specifier = strings.TrimSpace(specifier)
	if specifier == "" || strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") || strings.HasPrefix(specifier, "#") || strings.HasPrefix(specifier, "~") {
		return ""
	}
	bare := strings.TrimPrefix(specifier, "node:")
	if nodeBuiltins()[bare] || nodeBuiltins()[strings.Split(bare, "/")[0]] {
		return ""
	}
	if strings.HasPrefix(specifier, "@") {
		parts := strings.Split(specifier, "/")
		if len(parts) < 2 || parts[1] == "" {
			return ""
		}
		return strings.ToLower(parts[0] + "/" + parts[1])
	}
	return strings.ToLower(strings.Split(specifier, "/")[0])
}

func nodeBuiltins() map[string]bool {
	builtinOnce.Do(func() {
		var names []string
		if err := json.Unmarshal(data.NodeBuiltinsJSON, &names); err != nil {
			panic("embedded Node built-in dataset is invalid: " + err.Error())
		}
		builtins = make(map[string]bool, len(names))
		for _, name := range names {
			builtins[name] = true
		}
	})
	return builtins
}

func stripJSComments(source string) string {
	var result strings.Builder
	result.Grow(len(source))
	inLine, inBlock := false, false
	var quote byte
	escaped := false
	for i := 0; i < len(source); i++ {
		char := source[i]
		if inLine {
			if char == '\n' {
				inLine = false
				result.WriteByte(char)
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if inBlock {
			if char == '*' && i+1 < len(source) && source[i+1] == '/' {
				result.WriteString("  ")
				i++
				inBlock = false
			} else if char == '\n' {
				result.WriteByte('\n')
			} else {
				result.WriteByte(' ')
			}
			continue
		}
		if quote != 0 {
			result.WriteByte(char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' || char == '`' {
			quote = char
			result.WriteByte(char)
			continue
		}
		if char == '/' && i+1 < len(source) && source[i+1] == '/' {
			result.WriteString("  ")
			i++
			inLine = true
			continue
		}
		if char == '/' && i+1 < len(source) && source[i+1] == '*' {
			result.WriteString("  ")
			i++
			inBlock = true
			continue
		}
		result.WriteByte(char)
	}
	return result.String()
}
