package extractor

import (
	"regexp"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

var (
	pythonImport  = regexp.MustCompile(`(?m)^[\t ]*import[\t ]+([^#\r\n]+)`)
	pythonFrom    = regexp.MustCompile(`(?m)^[\t ]*from[\t ]+([A-Za-z_.][A-Za-z0-9_.]*)[\t ]+import[\t ]+`)
	pythonDynamic = regexp.MustCompile(`(?:importlib[\t ]*\.[\t ]*import_module|__import__)[\t ]*\([\t ]*["']([^"'\r\n]+)["']`)
)

// Python extracts top-level imports and literal dynamic imports using a conservative tokenizer.
func Python(path, content string) ([]model.Finding, int) {
	masked := maskPythonCommentsAndTripleStrings(content)
	var findings []model.Finding
	seen := make(map[string]bool)
	add := func(name string, offset int) {
		name = strings.Split(strings.TrimSpace(name), ".")[0]
		if !validPythonName(name) {
			return
		}
		line := lineForOffset(content, offset)
		key := name + "@" + string(rune(line))
		if seen[key] {
			return
		}
		seen[key] = true
		findings = append(findings, model.Finding{Name: name, Ecosystem: model.PyPI, Path: path, Line: line, Snippet: contextAt(content, line)})
	}
	for _, match := range pythonImport.FindAllStringSubmatchIndex(masked, -1) {
		for _, part := range strings.Split(masked[match[2]:match[3]], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) > 0 {
				add(fields[0], match[2])
			}
		}
	}
	for _, match := range pythonFrom.FindAllStringSubmatchIndex(masked, -1) {
		module := masked[match[2]:match[3]]
		if !strings.HasPrefix(module, ".") {
			add(module, match[2])
		}
	}
	unscannable := 0
	for _, match := range pythonDynamic.FindAllStringSubmatchIndex(masked, -1) {
		add(masked[match[2]:match[3]], match[2])
	}
	// A non-literal dynamic import cannot be statically proven. Count it without evaluating it.
	for _, line := range strings.Split(masked, "\n") {
		if (strings.Contains(line, "import_module(") || strings.Contains(line, "__import__(")) && !pythonDynamic.MatchString(line) {
			unscannable++
		}
	}
	return findings, unscannable
}

// maskPythonCommentsAndTripleStrings preserves byte offsets while removing non-code regions.
// Single-line strings remain intact so literal importlib calls can still be extracted.
func maskPythonCommentsAndTripleStrings(source string) string {
	masked := []byte(source)
	for index := 0; index < len(source); {
		if source[index] == '#' {
			for index < len(source) && source[index] != '\n' {
				masked[index] = ' '
				index++
			}
			continue
		}
		if index+2 < len(source) && (source[index:index+3] == "'''" || source[index:index+3] == `"""`) {
			delimiter := source[index : index+3]
			for offset := 0; offset < 3; offset++ {
				masked[index+offset] = ' '
			}
			index += 3
			for index < len(source) {
				if index+2 < len(source) && source[index:index+3] == delimiter {
					for offset := 0; offset < 3; offset++ {
						masked[index+offset] = ' '
					}
					index += 3
					break
				}
				if source[index] != '\n' {
					masked[index] = ' '
				}
				index++
			}
			continue
		}
		index++
	}
	return string(masked)
}

func validPythonName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if !(char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || index > 0 && char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func lineForOffset(content string, offset int) int { return strings.Count(content[:offset], "\n") + 1 }

func lineAt(content string, target int) string {
	lines := strings.Split(content, "\n")
	if target < 1 || target > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[target-1], "\r")
}

// contextAt keeps a small fixed source window for an explicitly opted-in remediation request.
func contextAt(content string, target int) string {
	lines := strings.Split(content, "\n")
	first, last := target-2, target+2
	if first < 1 {
		first = 1
	}
	if last > len(lines) {
		last = len(lines)
	}
	var result strings.Builder
	for line := first; line <= last; line++ {
		if line > first {
			result.WriteByte('\n')
		}
		result.WriteString(lineAt(content, line))
	}
	return result.String()
}
