// Package scanner joins extraction, false-positive filtering, cache, risk, and registry validation.
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phantomguard/phantomguard/data"
	"github.com/phantomguard/phantomguard/pkg/config"
	"github.com/phantomguard/phantomguard/pkg/extractor"
	pgmath "github.com/phantomguard/phantomguard/pkg/math"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

var pep503 = regexp.MustCompile(`[-_.]+`)

// Scanner has injectable dependencies so offline tests can use httptest and a temporary cache.
type Scanner struct {
	Root     string
	Config   config.Config
	Registry *validator.Client
	Cache    *validator.Cache
	Risk     *pgmath.RiskEngine
	aliases  map[string]string
}

// New constructs the production scan pipeline.
func New(root string, cfg config.Config, registry *validator.Client, cache *validator.Cache) *Scanner {
	if registry == nil {
		registry = validator.NewClient()
	}
	if cache == nil {
		var err error
		cache, err = validator.NewCache("")
		if err != nil {
			panic("initialize PhantomGuard cache: " + err.Error())
		}
	}
	aliases := bundledAliases()
	for key, value := range cfg.Aliases {
		aliases[strings.ToLower(key)] = value
	}
	return &Scanner{Root: root, Config: cfg, Registry: registry, Cache: cache, Risk: pgmath.NewRiskEngine(), aliases: aliases}
}

// ScanContents scans only caller-supplied immutable content (Git-index data in hook mode).
func (s *Scanner) ScanContents(ctx context.Context, contents map[string]string, modulePrefix string) (model.Report, error) {
	return s.scanContents(ctx, contents, contents, nil, modulePrefix, false)
}

// ScanContentsWithEvidence keeps candidate extraction limited to contents while
// allowing index-derived manifests and lockfiles to prove provenance. Evidence
// is never parsed as a new dependency candidate.
func (s *Scanner) ScanContentsWithEvidence(ctx context.Context, contents, evidence map[string]string, modulePrefix string) (model.Report, error) {
	return s.scanContents(ctx, contents, evidence, nil, modulePrefix, false)
}

// ScanIndexContents is the staged-only variant. indexPaths records every
// Git-index path while evidence contains only text needed for provenance and
// workspace/alias resolution, so local filtering never inspects the working
// tree without making provenance processing proportional to repository size.
func (s *Scanner) ScanIndexContents(ctx context.Context, contents, evidence map[string]string, indexPaths map[string]bool, modulePrefix string) (model.Report, error) {
	return s.scanContents(ctx, contents, evidence, indexPaths, modulePrefix, true)
}

func (s *Scanner) scanContents(ctx context.Context, contents, evidence map[string]string, indexPaths map[string]bool, modulePrefix string, indexOnly bool) (model.Report, error) {
	if evidence == nil {
		evidence = contents
	}
	report := model.Report{FilesScanned: len(contents), Results: []model.Result{}, Notices: []string{}}
	var extracted []model.Finding
	paths := make([]string, 0, len(contents))
	for path := range contents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if Ignored(path, s.Config.Ignore) || !s.Config.LanguageEnabled(extractor.Language(path)) {
			continue
		}
		pathModulePrefix := modulePrefix
		if extractor.Language(path) == "go" {
			pathModulePrefix = modulePrefixForPath(evidence, path, modulePrefix)
		}
		findings, notices, unscannable := extractor.Extract(path, contents[path], pathModulePrefix)
		extracted = append(extracted, findings...)
		report.Notices = append(report.Notices, notices...)
		report.AnalysisIncomplete = append(report.AnalysisIncomplete, notices...)
		report.Unscannable += unscannable
		if unscannable > 0 {
			report.AnalysisIncomplete = append(report.AnalysisIncomplete, fmt.Sprintf("%s: %d dynamic import(s) could not be statically resolved", path, unscannable))
		}
	}
	type activeFinding struct {
		finding     model.Finding
		packageName string
		status      model.Status
	}
	active := make([]activeFinding, 0, len(extracted))
	pending := make(map[validator.Candidate]bool)
	positiveTTL := time.Duration(s.Config.PositiveTTLHours) * time.Hour
	negativeTTL := time.Duration(s.Config.NegativeTTLHours) * time.Hour
	for _, finding := range extracted {
		packageName := s.Resolve(finding)
		local, err := s.localOrAllowed(finding, packageName, indexOnly, evidence, indexPaths)
		if err != nil {
			return report, fmt.Errorf("filter %s:%d: %w", finding.Path, finding.Line, err)
		}
		if local {
			continue
		}
		if finding.Ecosystem == model.Go {
			// Go's checksum database is reflected in go.sum. A matching module
			// requirement and checksum give strict mode integrity evidence without
			// making a separate, endpoint-configurable network request.
			status := model.Unknown
			if verified, _ := s.provenanceFor(evidence, finding, packageName); verified {
				status = model.Exists
			}
			active = append(active, activeFinding{finding, packageName, status})
			continue
		}
		if cached, ok := s.Cache.Get(finding.Ecosystem, packageName, positiveTTL, negativeTTL); ok {
			active = append(active, activeFinding{finding, packageName, cached})
			continue
		}
		candidate := validator.Candidate{Ecosystem: finding.Ecosystem, Name: packageName}
		pending[candidate] = true
		active = append(active, activeFinding{finding, packageName, ""})
	}
	candidates := make([]validator.Candidate, 0, len(pending))
	for candidate := range pending {
		candidates = append(candidates, candidate)
	}
	statuses := s.Registry.LookupMany(ctx, candidates)
	for _, candidate := range candidates {
		if err := s.Cache.Put(candidate.Ecosystem, candidate.Name, statuses[candidate]); err != nil {
			report.Notices = append(report.Notices, "cache not updated: "+err.Error())
		}
	}
	for _, item := range active {
		status := item.status
		if status == "" {
			status = statuses[validator.Candidate{Ecosystem: item.finding.Ecosystem, Name: item.packageName}]
		}
		provenanceVerified, provenance := s.provenanceFor(evidence, item.finding, item.packageName)
		result := model.Result{Finding: item.finding, Package: item.packageName, Status: status, Provenance: provenance, ProvenanceVerified: provenanceVerified}
		if status != model.Exists && item.finding.Ecosystem != model.Go {
			result.Suggestions = s.Risk.Suggestions(item.finding.Ecosystem, item.packageName, 3)
		}
		if status == model.Phantom {
			if match, high := s.Risk.Assess(item.finding.Ecosystem, item.packageName); high {
				result.RiskMatch = match
			}
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

// modulePrefixForPath selects the nearest indexed go.mod for a Go source file.
// A repository can contain nested modules; using only the root module would
// incorrectly send a nested module's own imports through public validation.
func modulePrefixForPath(evidence map[string]string, sourcePath, fallback string) string {
	for _, goModPath := range scopedEvidencePaths(evidence, sourcePath, isGoModPath) {
		if prefix := extractor.ModulePath(evidence[goModPath]); prefix != "" {
			return prefix
		}
	}
	return fallback
}

// Resolve maps imports to registry names and applies PEP 503 only to PyPI packages.
func (s *Scanner) Resolve(finding model.Finding) string {
	name := finding.Name
	if mapped, ok := s.aliases[strings.ToLower(name)]; ok {
		name = mapped
	}
	if finding.Ecosystem == model.PyPI {
		return pep503.ReplaceAllString(strings.ToLower(name), "-")
	}
	return strings.ToLower(name)
}

func bundledAliases() map[string]string {
	var source map[string]string
	if err := json.Unmarshal(data.AliasesJSON, &source); err != nil {
		panic("embedded aliases are invalid: " + err.Error())
	}
	aliases := make(map[string]string, len(source))
	for key, value := range source {
		aliases[strings.ToLower(key)] = value
	}
	return aliases
}

// LocalOrAllowed removes reviewed internal dependencies before any public network query.
func (s *Scanner) LocalOrAllowed(finding model.Finding, packageName string) (bool, error) {
	return s.localOrAllowed(finding, packageName, false, nil, nil)
}

func (s *Scanner) localOrAllowed(finding model.Finding, packageName string, indexOnly bool, evidence map[string]string, indexPaths map[string]bool) (bool, error) {
	for _, allowed := range s.Config.Allow {
		if strings.EqualFold(allowed, finding.Name) || strings.EqualFold(allowed, packageName) {
			return true, nil
		}
	}
	switch finding.Ecosystem {
	case model.PyPI:
		if indexOnly {
			return pythonStdlib[strings.ToLower(finding.Name)] || localPythonInIndex(indexPaths, finding.Name), nil
		}
		return pythonStdlib[strings.ToLower(finding.Name)] || localPython(s.Root, finding.Name), nil
	case model.NPM:
		if indexOnly {
			if workspace, err := workspacePackageInIndex(evidence, finding.Name); err != nil || workspace {
				return workspace, err
			}
			return tsPathAliasInIndex(evidence, finding.Name)
		}
		if workspace, err := workspacePackage(s.Root, finding.Name); err != nil || workspace {
			return workspace, err
		}
		return tsPathAlias(s.Root, finding.Name)
	default:
		return false, nil
	}
}

// Ignored implements **, *, and ? path patterns against repository-relative slash paths.
func Ignored(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range patterns {
		if globMatch(path, filepath.ToSlash(pattern)) {
			return true
		}
	}
	return false
}

// IncompleteNotices selects input-collection notices that mean a supported
// candidate file was not analyzed. Callers retain the original notice for
// warn-mode visibility and add these reasons to Report.AnalysisIncomplete so
// strict mode can fail closed without treating cache diagnostics as failures.
func IncompleteNotices(notices []string) []string {
	var incomplete []string
	for _, notice := range notices {
		if strings.Contains(notice, ": skipped (") {
			incomplete = append(incomplete, notice)
		}
	}
	return incomplete
}

func globMatch(value, pattern string) bool {
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		char := pattern[index]
		switch char {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index++
				if index+1 < len(pattern) && pattern[index+1] == '/' {
					expression.WriteString("(?:.*/)?")
					index++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			expression.WriteByte('\\')
			expression.WriteByte(char)
		default:
			expression.WriteByte(char)
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), value)
	return err == nil && matched
}

func localPython(root, name string) bool {
	for _, base := range []string{root, filepath.Join(root, "src")} {
		if exists(filepath.Join(base, name+".py")) || exists(filepath.Join(base, name, "__init__.py")) {
			return true
		}
	}
	return false
}

func exists(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }

func localPythonInIndex(indexPaths map[string]bool, name string) bool {
	for _, path := range []string{name + ".py", filepath.Join(name, "__init__.py"), filepath.Join("src", name+".py"), filepath.Join("src", name, "__init__.py")} {
		if indexPathExists(indexPaths, path) {
			return true
		}
	}
	return false
}

func indexPathExists(indexPaths map[string]bool, path string) bool {
	return indexPaths[normaliseIndexPath(path)]
}

func normaliseIndexPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	return strings.TrimPrefix(path, "./")
}

func workspacePackage(root, name string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read package workspaces: %w", err)
	}
	var document struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return false, fmt.Errorf("parse package workspaces: %w", err)
	}
	var patterns []string
	if len(document.Workspaces) > 0 {
		if err := json.Unmarshal(document.Workspaces, &patterns); err != nil {
			var object struct {
				Packages []string `json:"packages"`
			}
			if err := json.Unmarshal(document.Workspaces, &object); err != nil {
				return false, fmt.Errorf("parse package workspaces: %w", err)
			}
			patterns = object.Packages
		}
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern, "package.json"))
		if err != nil {
			return false, fmt.Errorf("invalid workspace glob %q: %w", pattern, err)
		}
		for _, manifest := range matches {
			contents, err := os.ReadFile(manifest)
			if err != nil {
				return false, err
			}
			var packageDocument struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(contents, &packageDocument); err != nil {
				return false, err
			}
			if strings.EqualFold(packageDocument.Name, name) {
				return true, nil
			}
		}
	}
	return false, nil
}

func workspacePackageInIndex(index map[string]string, name string) (bool, error) {
	raw, ok := index["package.json"]
	if !ok {
		return false, nil
	}
	var document struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return false, fmt.Errorf("parse index package workspaces: %w", err)
	}
	var patterns []string
	if len(document.Workspaces) > 0 {
		if err := json.Unmarshal(document.Workspaces, &patterns); err != nil {
			var object struct {
				Packages []string `json:"packages"`
			}
			if err := json.Unmarshal(document.Workspaces, &object); err != nil {
				return false, fmt.Errorf("parse index package workspaces: %w", err)
			}
			patterns = object.Packages
		}
	}
	for _, pattern := range patterns {
		manifestPattern := normaliseIndexPath(filepath.Join(pattern, "package.json"))
		for path, contents := range index {
			if path == "package.json" || !strings.HasSuffix(strings.ToLower(path), "/package.json") || !globMatch(path, manifestPattern) {
				continue
			}
			var packageDocument struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(contents), &packageDocument); err != nil {
				return false, fmt.Errorf("parse index workspace %s: %w", path, err)
			}
			if strings.EqualFold(packageDocument.Name, name) {
				return true, nil
			}
		}
	}
	return false, nil
}

func tsPathAlias(root, name string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var document struct {
		CompilerOptions struct {
			Paths map[string]json.RawMessage `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return false, fmt.Errorf("parse tsconfig paths: %w", err)
	}
	for alias := range document.CompilerOptions.Paths {
		alias = strings.TrimSuffix(alias, "/*")
		if strings.EqualFold(alias, name) {
			return true, nil
		}
	}
	return false, nil
}

func tsPathAliasInIndex(index map[string]string, name string) (bool, error) {
	raw, ok := index["tsconfig.json"]
	if !ok {
		return false, nil
	}
	var document struct {
		CompilerOptions struct {
			Paths map[string]json.RawMessage `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return false, fmt.Errorf("parse index tsconfig paths: %w", err)
	}
	for alias := range document.CompilerOptions.Paths {
		alias = strings.TrimSuffix(alias, "/*")
		if strings.EqualFold(alias, name) {
			return true, nil
		}
	}
	return false, nil
}

var pythonStdlib = stringSet(`
__future__ abc argparse array ast atexit base64 bdb binascii bisect builtins bz2 calendar cmath cmd code codecs codeop collections colorsys compileall concurrent configparser contextlib contextvars copy copyreg csv ctypes curses dataclasses datetime dbm decimal difflib dis doctest email encodings enum errno faulthandler filecmp fileinput fnmatch fractions ftplib functools gc getopt getpass gettext glob graphlib grp gzip hashlib heapq hmac html http idlelib imaplib importlib inspect io ipaddress itertools json keyword linecache locale logging lzma mailbox marshal math mimetypes mmap modulefinder msvcrt multiprocessing netrc numbers opcode operator optparse os pathlib pdb pickle pickletools pkgutil platform plistlib poplib posix pprint profile pstats pty pwd py_compile pyclbr pydoc queue quopri random re readline reprlib resource runpy sched secrets select selectors shelve shlex shutil signal site smtplib socket socketserver sqlite3 sre_compile sre_constants sre_parse ssl stat statistics string stringprep struct subprocess symtable sys sysconfig syslog tabnanny tarfile tempfile termios textwrap threading time timeit tkinter token tokenize tomllib trace traceback tracemalloc tty turtle types typing unicodedata unittest urllib uuid venv warnings wave weakref webbrowser winreg winsound wsgiref xml xmlrpc zipapp zipfile zipimport zlib zoneinfo
`)

func stringSet(values string) map[string]bool {
	result := make(map[string]bool)
	for _, value := range strings.Fields(values) {
		result[value] = true
	}
	return result
}
