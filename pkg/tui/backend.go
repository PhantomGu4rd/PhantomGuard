package tui

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/phantomguard/phantomguard/pkg/config"
	"github.com/phantomguard/phantomguard/pkg/extractor"
	"github.com/phantomguard/phantomguard/pkg/gitio"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/remediation"
	"github.com/phantomguard/phantomguard/pkg/report"
	"github.com/phantomguard/phantomguard/pkg/scanner"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

const maxInputFileSize = 1 << 20

// Target is one of the two safe scan scopes exposed by the terminal workspace.
type Target string

const (
	TargetStaged Target = "staged"
	TargetAll    Target = "all"
	TargetPaths  Target = "paths"
)

// ScanRequest matches the supported manual scan scopes. Strict changes policy
// for this invocation only; it never edits repository configuration.
type ScanRequest struct {
	Target Target
	Paths  []string
	Strict bool
}

// Status describes the local repository and the active policy.
type Status struct {
	Repository string
	Branch     string
	FailMode   string
}

// ScanResult is scanner output plus the policy decision used by the TUI.
type ScanResult struct {
	Report   model.Report
	FailMode string
	Blocked  bool
	Skipped  bool
	Duration time.Duration
}

// CacheStatus is intentionally aggregate-only: the TUI never exposes the
// cached package names or filesystem location.
type CacheStatus struct {
	Entries          int
	Verified         int
	Phantom          int
	PositiveTTLHours int
	NegativeTTLHours int
}

// FixRequest contains the same explicit parameters accepted by `phantomguard fix`.
type FixRequest struct {
	Path      string
	From      string
	To        string
	Ecosystem model.Ecosystem
}

// Backend is intentionally small so the terminal presentation can be tested
// without a registry. ScannerBackend is the production implementation.
type Backend interface {
	Status(context.Context) (Status, error)
	Scan(context.Context, ScanRequest) (ScanResult, error)
	CacheStatus(context.Context) (CacheStatus, error)
	ClearCache(context.Context) error
	Install(context.Context, bool) (string, error)
	Fix(context.Context, FixRequest, io.Reader, io.Writer) error
}

// ScannerBackend connects the TUI directly to PhantomGuard's existing scan
// pipeline: config, static extractors, registry client, cache, risk engine and
// verdict policy are exactly the same components used by the hook and CLI.
type ScannerBackend struct {
	root     string
	registry *validator.Client
	cache    *validator.Cache
}

// NewScannerBackend creates a production backend rooted in an existing Git repository.
func NewScannerBackend(root string) (*ScannerBackend, error) {
	cache, err := validator.NewCache("")
	if err != nil {
		return nil, fmt.Errorf("initialize cache: %w", err)
	}
	return &ScannerBackend{root: root, registry: validator.NewClient(), cache: cache}, nil
}

// Status returns real repository state; it never performs a network request.
func (b *ScannerBackend) Status(context.Context) (Status, error) {
	cfg, err := config.Load(b.root, false)
	if err != nil {
		return Status{}, fmt.Errorf("read configuration: %w", err)
	}
	return Status{
		Repository: filepath.Base(b.root),
		Branch:     currentBranch(b.root),
		FailMode:   cfg.FailMode,
	}, nil
}

// Scan uses immutable Git-index content for staged scans and the existing
// scanner package for all validation, caching, suggestions, and policy logic.
func (b *ScannerBackend) Scan(ctx context.Context, request ScanRequest) (ScanResult, error) {
	if os.Getenv("PHANTOMGUARD_SKIP") == "1" {
		return ScanResult{Skipped: true}, nil
	}
	var cfg config.Config
	var err error
	if request.Target == TargetStaged {
		cfg, err = config.LoadIndex(b.root, request.Strict)
	} else {
		cfg, err = config.Load(b.root, request.Strict)
	}
	if err != nil {
		return ScanResult{}, fmt.Errorf("configuration: %w", err)
	}
	var contents map[string]string
	var evidence map[string]string
	var indexPaths map[string]bool
	var notices []string
	var modulePrefix string
	switch request.Target {
	case TargetStaged:
		if len(request.Paths) != 0 {
			return ScanResult{}, fmt.Errorf("staged scan accepts no paths")
		}
		contents, notices, modulePrefix, err = stagedContents(b.root, cfg.Ignore)
		if err == nil {
			var evidenceNotices []string
			evidence, indexPaths, evidenceNotices, err = stagedIndexEvidence(b.root, contents)
			notices = append(notices, evidenceNotices...)
		}
	case TargetAll:
		if len(request.Paths) != 0 {
			return ScanResult{}, fmt.Errorf("all-files scan accepts no paths")
		}
		contents, notices, modulePrefix, err = workingContents(b.root, cfg.Ignore)
	case TargetPaths:
		if len(request.Paths) == 0 {
			return ScanResult{}, fmt.Errorf("choose at least one repository-relative path")
		}
		contents, notices, modulePrefix, err = selectedContents(b.root, request.Paths)
	default:
		return ScanResult{}, fmt.Errorf("unsupported scan target %q", request.Target)
	}
	if err != nil {
		return ScanResult{}, fmt.Errorf("scan inputs: %w", err)
	}
	if evidence == nil {
		evidence = contents
	}

	started := time.Now()
	checkerConfig := cfg
	if request.Target == TargetPaths {
		// An explicit TUI path is a direct maintainer request, so do not let
		// automatic ignore patterns hide an intentionally selected fixture.
		checkerConfig.Ignore = nil
	}
	checker := scanner.New(b.root, checkerConfig, b.registry, b.cache)
	var result model.Report
	if request.Target == TargetStaged {
		result, err = checker.ScanIndexContents(ctx, contents, evidence, indexPaths, modulePrefix)
	} else {
		result, err = checker.ScanContentsWithEvidence(ctx, contents, evidence, modulePrefix)
	}
	if err != nil {
		return ScanResult{}, err
	}
	result.Notices = append(result.Notices, notices...)
	result.AnalysisIncomplete = append(result.AnalysisIncomplete, scanner.IncompleteNotices(notices)...)
	return ScanResult{
		Report:   result,
		FailMode: cfg.FailMode,
		Blocked:  report.Blocked(result, cfg.FailMode),
		Duration: time.Since(started),
	}, nil
}

// CacheStatus returns aggregate cache health and the active retention policy.
func (b *ScannerBackend) CacheStatus(context.Context) (CacheStatus, error) {
	cfg, err := config.Load(b.root, false)
	if err != nil {
		return CacheStatus{}, fmt.Errorf("read configuration: %w", err)
	}
	stats := b.cache.Stats()
	return CacheStatus{
		Entries:          stats.Entries,
		Verified:         stats.Exists,
		Phantom:          stats.Phantom,
		PositiveTTLHours: cfg.PositiveTTLHours,
		NegativeTTLHours: cfg.NegativeTTLHours,
	}, nil
}

// ClearCache removes local registry answers only. Source files and repository
// configuration are never touched.
func (b *ScannerBackend) ClearCache(context.Context) error { return b.cache.Clear() }

// Install delegates to the same hook installer used by the regular CLI.
func (b *ScannerBackend) Install(_ context.Context, force bool) (string, error) {
	return gitio.InstallHook(b.root, force)
}

// Fix retains the existing verified-preview-confirm-revalidate flow. The TUI
// deliberately does not auto-apply replacements.
func (b *ScannerBackend) Fix(ctx context.Context, request FixRequest, input io.Reader, output io.Writer) error {
	return remediation.PreviewAndApply(ctx, b.root, remediation.Fix{
		Path:      request.Path,
		From:      request.From,
		To:        request.To,
		Ecosystem: request.Ecosystem,
	}, b.registry, input, output)
}

func currentBranch(root string) string {
	return gitOutput(root, "branch", "--show-current", "detached")
}

func currentCommit(root string) string {
	return gitOutput(root, "rev-parse", "--short", "HEAD", "local")
}

func gitOutput(root string, arguments ...string) string {
	fallback := arguments[len(arguments)-1]
	arguments = arguments[:len(arguments)-1]
	command := exec.Command("git", arguments...)
	command.Dir = root
	output, err := command.Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return fallback
	}
	return strings.TrimSpace(string(output))
}

func stagedContents(root string, ignores []string) (map[string]string, []string, string, error) {
	contents := make(map[string]string)
	var notices []string
	paths, err := gitio.StagedFiles(root)
	if err != nil {
		return nil, nil, "", err
	}
	for _, path := range paths {
		if !extractor.Relevant(path) {
			continue
		}
		if scanner.Ignored(path, ignores) {
			notices = append(notices, path+": ignored by .phantomguard.json")
			continue
		}
		raw, err := gitio.StagedContent(root, path)
		if err != nil {
			return nil, nil, "", err
		}
		if len(raw) > maxInputFileSize {
			notices = append(notices, path+": skipped (>1 MiB)")
			continue
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			notices = append(notices, path+": skipped (binary or non-UTF-8 staged content)")
			continue
		}
		contents[path] = content
	}
	return contents, notices, stagedModulePrefix(root), nil
}

// stagedIndexEvidence represents every Git-index path and reads the manifests
// needed for provenance and local-package filtering without broadening the
// staged candidate set.
func stagedIndexEvidence(root string, contents map[string]string) (map[string]string, map[string]bool, []string, error) {
	evidence := make(map[string]string, len(contents))
	for path, content := range contents {
		evidence[path] = content
	}
	paths, err := gitio.IndexFiles(root)
	if err != nil {
		return nil, nil, nil, err
	}
	indexPaths := make(map[string]bool, len(paths))
	var notices []string
	for _, path := range paths {
		indexPaths[path] = true
		if _, alreadyIncluded := evidence[path]; alreadyIncluded {
			continue
		}
		if !extractor.IndexContextRelevant(path) {
			continue
		}
		raw, err := gitio.StagedContent(root, path)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(raw) > maxInputFileSize {
			notices = append(notices, path+": index context skipped (>1 MiB)")
			continue
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			notices = append(notices, path+": index context skipped (binary or non-UTF-8 index content)")
			continue
		}
		evidence[path] = content
	}
	return evidence, indexPaths, notices, nil
}

func stagedModulePrefix(root string) string {
	raw, err := gitio.StagedContent(root, "go.mod")
	if err != nil {
		return ""
	}
	content, ok := gitio.DecodeUTF8(raw)
	if !ok {
		return ""
	}
	return extractor.ModulePath(content)
}

func workingContents(root string, ignores []string) (map[string]string, []string, string, error) {
	contents := make(map[string]string)
	var notices []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				relative = path
			}
			notices = append(notices, filepath.ToSlash(relative)+": skipped ("+walkErr.Error()+")")
			return nil
		}
		if entry.IsDir() {
			if toolCacheDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if scanner.Ignored(filepath.ToSlash(relative)+"/.phantomguard-ignore", ignores) {
				notices = append(notices, filepath.ToSlash(relative)+"/: ignored by .phantomguard.json")
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.Clean(relative)
		if !extractor.Relevant(relative) {
			return nil
		}
		if scanner.Ignored(filepath.ToSlash(relative), ignores) {
			notices = append(notices, filepath.ToSlash(relative)+": ignored by .phantomguard.json")
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(raw) > maxInputFileSize {
			notices = append(notices, filepath.ToSlash(relative)+": skipped (>1 MiB)")
			return nil
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			notices = append(notices, filepath.ToSlash(relative)+": skipped (binary or non-UTF-8 content)")
			return nil
		}
		contents[filepath.ToSlash(relative)] = content
		return nil
	})
	if err != nil {
		return nil, nil, "", err
	}
	modulePrefix := ""
	if raw, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		if content, ok := gitio.DecodeUTF8(raw); ok {
			modulePrefix = extractor.ModulePath(content)
		}
	}
	return contents, notices, modulePrefix, nil
}

// selectedContents honours an explicit path request even when it matches an
// automatic ignore pattern. This is useful for deliberately unsafe fixtures:
// they stay out of routine scans and enforcement, but remain inspectable on
// demand from the TUI.
func selectedContents(root string, selected []string) (map[string]string, []string, string, error) {
	contents := make(map[string]string)
	var notices []string
	for _, relative := range selected {
		relative = filepath.Clean(relative)
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, "", fmt.Errorf("path escapes repository: %s", relative)
		}
		if !extractor.Relevant(relative) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			if os.IsNotExist(err) {
				notices = append(notices, filepath.ToSlash(relative)+": skipped (not found)")
				continue
			}
			return nil, nil, "", err
		}
		if len(raw) > maxInputFileSize {
			notices = append(notices, filepath.ToSlash(relative)+": skipped (>1 MiB)")
			continue
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			notices = append(notices, filepath.ToSlash(relative)+": skipped (binary or non-UTF-8 content)")
			continue
		}
		contents[filepath.ToSlash(relative)] = content
	}
	modulePrefix := ""
	if raw, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		if content, ok := gitio.DecodeUTF8(raw); ok {
			modulePrefix = extractor.ModulePath(content)
		}
	}
	return contents, notices, modulePrefix, nil
}

func toolCacheDirectory(name string) bool {
	switch name {
	case ".git", ".pytest_cache", ".ruff_cache", "node_modules", ".mypy_cache", ".tox", "bin", "dist":
		return true
	default:
		return false
	}
}
