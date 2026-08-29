package scanner

import (
	"encoding/json"
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

var requirementName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*`)

func (s *Scanner) provenanceFor(contents map[string]string, finding model.Finding, packageName string) (bool, string) {
	switch finding.Ecosystem {
	case model.PyPI:
		return pythonProvenance(contents, finding.Path, packageName)
	case model.NPM:
		return npmProvenance(contents, finding.Path, packageName)
	case model.Go:
		return goProvenance(contents, finding.Path, packageName)
	default:
		return false, ""
	}
}

// scopedEvidencePaths returns evidence in the dependency's own directory and
// then its ancestors. A sibling workspace lockfile must never substantiate a
// finding from another workspace. Sorting makes both the verdict and the
// rendered evidence stable across Go map iteration orders.
func scopedEvidencePaths(contents map[string]string, findingPath string, matches func(string) bool) []string {
	var paths []string
	for _, directory := range evidenceDirectories(findingPath) {
		var inDirectory []string
		for candidate := range contents {
			if evidenceDir(candidate) == directory && matches(candidate) {
				inDirectory = append(inDirectory, candidate)
			}
		}
		sort.Strings(inDirectory)
		paths = append(paths, inDirectory...)
	}
	return paths
}

func evidenceDirectories(findingPath string) []string {
	directory := evidenceDir(findingPath)
	directories := make([]string, 0, 4)
	for {
		directories = append(directories, directory)
		if directory == "" {
			return directories
		}
		parent := evidenceDir(directory)
		if parent == directory {
			return directories
		}
		directory = parent
	}
}

func evidenceDir(path string) string {
	directory := pathpkg.Dir(normaliseEvidencePath(path))
	if directory == "." {
		return ""
	}
	return directory
}

func evidencePath(directory, name string) string {
	if directory == "" {
		return name
	}
	return pathpkg.Join(directory, name)
}

func evidenceContent(contents map[string]string, wanted string) (string, bool) {
	wanted = normaliseEvidencePath(wanted)
	if content, ok := contents[wanted]; ok {
		return content, true
	}
	for path, content := range contents {
		if normaliseEvidencePath(path) == wanted {
			return content, true
		}
	}
	return "", false
}

func normaliseEvidencePath(value string) string {
	value = pathpkg.Clean(strings.ReplaceAll(value, "\\", "/"))
	if value == "." {
		return ""
	}
	return strings.TrimPrefix(value, "./")
}

func isRequirementsPath(path string) bool {
	base := strings.ToLower(pathpkg.Base(normaliseEvidencePath(path)))
	return strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")
}

func isPythonLockPath(path string) bool {
	switch strings.ToLower(pathpkg.Base(normaliseEvidencePath(path))) {
	case "pipfile.lock", "poetry.lock", "uv.lock":
		return true
	default:
		return false
	}
}

func isPackageJSONPath(path string) bool {
	return strings.EqualFold(pathpkg.Base(normaliseEvidencePath(path)), "package.json")
}

func isGoModPath(path string) bool {
	return strings.EqualFold(pathpkg.Base(normaliseEvidencePath(path)), "go.mod")
}

func pythonProvenance(contents map[string]string, findingPath, packageName string) (bool, string) {
	normalized := normalizePyPIName(packageName)
	if normalized == "" {
		return false, "no provenance evidence"
	}
	if verified, evidence := pythonLockProvenance(contents, findingPath, normalized); evidence != "" {
		return verified, evidence
	}
	for _, path := range scopedEvidencePaths(contents, findingPath, isRequirementsPath) {
		content := contents[path]
		for index, raw := range strings.Split(content, "\n") {
			line := strings.TrimSpace(strings.Split(strings.Split(raw, "#")[0], ";")[0])
			if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, ".") || strings.HasPrefix(line, "/") || strings.Contains(line, "://") || strings.HasPrefix(line, "git+") {
				continue
			}
			name := requirementName.FindString(line)
			if name == "" || normalizePyPIName(name) != normalized {
				continue
			}
			location := fmt.Sprintf("%s:%d", path, index+1)
			switch {
			case strings.Contains(line, "--hash=") && hasExactPin(line):
				return true, "hash-pinned via " + location
			case hasExactPin(line):
				return false, "weak provenance: version-pinned via " + location
			case hasFloatingPin(line):
				return false, "weak provenance: floating requirement in " + location
			default:
				return false, "weak provenance: requirement matched without a version pin in " + location
			}
		}
	}
	return false, "no provenance evidence"
}

func pythonLockProvenance(contents map[string]string, findingPath, normalizedPackage string) (bool, string) {
	for _, path := range scopedEvidencePaths(contents, findingPath, isPythonLockPath) {
		content := contents[path]
		switch strings.ToLower(pathpkg.Base(path)) {
		case "pipfile.lock":
			if verified, evidence := pipfileLockProvenance(path, content, normalizedPackage); evidence != "" {
				return verified, evidence
			}
		case "poetry.lock", "uv.lock":
			if verified, evidence := tomlPythonLockProvenance(path, content, normalizedPackage); evidence != "" {
				return verified, evidence
			}
		}
	}
	return false, ""
}

type pythonLockEntry struct {
	Version string   `json:"version"`
	Hashes  []string `json:"hashes"`
}

func pipfileLockProvenance(path, content, normalizedPackage string) (bool, string) {
	var document struct {
		Default map[string]pythonLockEntry `json:"default"`
		Develop map[string]pythonLockEntry `json:"develop"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return false, ""
	}
	for _, section := range []struct {
		name    string
		entries map[string]pythonLockEntry
	}{
		{name: "default", entries: document.Default},
		{name: "develop", entries: document.Develop},
	} {
		for name, entry := range section.entries {
			if normalizePyPIName(name) != normalizedPackage {
				continue
			}
			if len(entry.Hashes) > 0 {
				return true, fmt.Sprintf("hash-pinned via %s[%s]", path, section.name)
			}
			if strings.TrimSpace(entry.Version) != "" {
				return false, fmt.Sprintf("weak provenance: version-locked via %s[%s]", path, section.name)
			}
		}
	}
	return false, ""
}

func tomlPythonLockProvenance(path, content, normalizedPackage string) (bool, string) {
	var currentName string
	var currentVersion string
	hasHashes := false
	flush := func() (bool, string) {
		if normalizePyPIName(currentName) != normalizedPackage {
			currentName = ""
			currentVersion = ""
			hasHashes = false
			return false, ""
		}
		if hasHashes {
			return true, fmt.Sprintf("hash-pinned via %s", path)
		}
		if strings.TrimSpace(currentVersion) != "" {
			return false, fmt.Sprintf("weak provenance: version-locked via %s", path)
		}
		currentName = ""
		currentVersion = ""
		hasHashes = false
		return false, ""
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if verified, evidence := flush(); evidence != "" {
				return verified, evidence
			}
			continue
		}
		if strings.HasPrefix(line, "[[") || (strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")) {
			if verified, evidence := flush(); evidence != "" {
				return verified, evidence
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "name = "):
			currentName = trimPythonLockValue(strings.TrimPrefix(line, "name = "))
		case strings.HasPrefix(line, "version = "):
			currentVersion = trimPythonLockValue(strings.TrimPrefix(line, "version = "))
		case strings.HasPrefix(line, "hash = ") || strings.HasPrefix(line, "hashes = ") || strings.Contains(line, "sha256:"):
			hasHashes = true
		}
	}
	return flush()
}

func npmProvenance(contents map[string]string, findingPath, packageName string) (bool, string) {
	for _, path := range scopedEvidencePaths(contents, findingPath, isPackageJSONPath) {
		content := contents[path]
		spec, found := packageJSONSpec(content, packageName)
		if !found {
			continue
		}
		dir := evidenceDir(path)
		lockPath, lockVersion, lockIntegrity, locked := npmLockEvidence(contents, dir, packageName)
		location := fmt.Sprintf("%s dependency %q", path, packageName)
		switch {
		case locked && lockIntegrity != "" && lockVersion != "":
			return true, fmt.Sprintf("integrity-backed via %s (%s @ %s)", lockPath, packageName, lockVersion)
		case locked && lockVersion != "":
			return false, fmt.Sprintf("weak provenance: lockfile-backed via %s (%s @ %s)", lockPath, packageName, lockVersion)
		case hasExactVersionSpec(spec):
			return false, "weak provenance: pinned in " + location + " without lockfile evidence"
		case spec != "":
			return false, "weak provenance: floating manifest spec in " + location
		default:
			return false, "no provenance evidence"
		}
	}
	return false, "no provenance evidence"
}

func goProvenance(contents map[string]string, findingPath, importPath string) (bool, string) {
	for _, goModPath := range scopedEvidencePaths(contents, findingPath, isGoModPath) {
		content := contents[goModPath]
		bestModule := ""
		bestVersion := ""
		for _, req := range parseGoRequires(content) {
			if importPath == req.Module || strings.HasPrefix(importPath, req.Module+"/") {
				if len(req.Module) > len(bestModule) {
					bestModule = req.Module
					bestVersion = req.Version
				}
			}
		}
		if bestModule == "" {
			continue
		}
		sumPath := evidencePath(evidenceDir(goModPath), "go.sum")
		content, ok := evidenceContent(contents, sumPath)
		if !ok {
			return false, fmt.Sprintf("weak provenance: go.mod require without go.sum evidence via %s", goModPath)
		}
		if goSumHas(content, bestModule, bestVersion) {
			return true, fmt.Sprintf("go.sum-backed via %s and %s", goModPath, sumPath)
		}
		return false, fmt.Sprintf("weak provenance: go.mod require without matching checksum via %s", goModPath)
	}
	return false, "no provenance evidence"
}

type goRequire struct {
	Module  string
	Version string
}

func parseGoRequires(content string) []goRequire {
	var requires []goRequire
	inRequireBlock := false
	for _, raw := range strings.Split(content, "\n") {
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
		requires = append(requires, goRequire{Module: fields[0], Version: fields[1]})
	}
	return requires
}

func goSumHas(content, module, version string) bool {
	for _, raw := range strings.Split(content, "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 3 {
			continue
		}
		if fields[0] != module {
			continue
		}
		if fields[1] == version {
			return true
		}
	}
	return false
}

func packageJSONSpec(content, packageName string) (string, bool) {
	var document struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return "", false
	}
	for _, section := range []map[string]string{document.Dependencies, document.DevDependencies, document.PeerDependencies, document.OptionalDependencies} {
		if spec, ok := section[packageName]; ok {
			return strings.TrimSpace(spec), true
		}
	}
	return "", false
}

func npmLockEvidence(contents map[string]string, dir, packageName string) (string, string, string, bool) {
	for _, candidate := range []string{"package-lock.json", "npm-shrinkwrap.json"} {
		lockPath := evidencePath(dir, candidate)
		content, ok := evidenceContent(contents, lockPath)
		if !ok {
			continue
		}
		version, integrity, found := parseNPMLock(content, packageName)
		if found {
			return lockPath, version, integrity, true
		}
	}
	return "", "", "", false
}

func parseNPMLock(content, packageName string) (string, string, bool) {
	var document struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return "", "", false
	}
	for _, preferred := range []string{packageName, "node_modules/" + packageName} {
		if entry, ok := document.Packages[preferred]; ok {
			return entry.Version, entry.Integrity, true
		}
	}
	var keys []string
	for key := range document.Packages {
		if npmLockKeyMatches(key, packageName) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		entry := document.Packages[keys[0]]
		return entry.Version, entry.Integrity, true
	}
	if entry, ok := document.Dependencies[packageName]; ok {
		return entry.Version, entry.Integrity, true
	}
	return "", "", false
}

func npmLockKeyMatches(key, packageName string) bool {
	key = pathpkg.Clean(strings.ReplaceAll(key, "\\", "/"))
	packageName = pathpkg.Clean(strings.ReplaceAll(packageName, "\\", "/"))
	if key == packageName || key == "node_modules/"+packageName {
		return true
	}
	return strings.HasSuffix(key, "/node_modules/"+packageName)
}

func hasExactPin(spec string) bool {
	spec = strings.TrimSpace(spec)
	return strings.Contains(spec, "===") || strings.Contains(spec, "==")
}

func hasFloatingPin(spec string) bool {
	spec = strings.TrimSpace(spec)
	return strings.ContainsAny(spec, "<>~!*") || strings.Contains(spec, "||")
}

func hasExactVersionSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	for _, prefix := range []string{"file:", "link:", "workspace:", "git+", "github:", "http://", "https://", "ssh:", "^", "~", ">", "<", "*"} {
		if strings.HasPrefix(strings.ToLower(spec), prefix) {
			return false
		}
	}
	if strings.Contains(spec, "||") {
		return false
	}
	return true
}

func trimPythonLockValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(strings.TrimPrefix(value, "\""), "\"")
	value = strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	return strings.TrimSpace(value)
}

func normalizePyPIName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	return pep503.ReplaceAllString(name, "-")
}
