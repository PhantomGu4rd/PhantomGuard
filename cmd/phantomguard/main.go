// PhantomGuard blocks staged phantom dependencies before Git records them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
	"github.com/phantomguard/phantomguard/pkg/config"
	"github.com/phantomguard/phantomguard/pkg/extractor"
	"github.com/phantomguard/phantomguard/pkg/gitio"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/remediation"
	"github.com/phantomguard/phantomguard/pkg/report"
	"github.com/phantomguard/phantomguard/pkg/scanner"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

const maxFileSize = 1 << 20

func main() { os.Exit(run(os.Args[1:])) }

func run(arguments []string) int {
	if len(arguments) == 0 {
		usage(os.Stderr)
		return 2
	}
	// Help and cache maintenance do not inspect a repository, so keep them
	// usable from arbitrary directories (including a bare container shell).
	switch arguments[0] {
	case "--help", "help", "-h":
		usage(os.Stdout)
		return 0
	case "--version", "version", "-v":
		if len(arguments) != 1 {
			fmt.Fprintln(os.Stderr, "PhantomGuard: version accepts no arguments")
			return 2
		}
		fmt.Fprintln(os.Stdout, buildinfo.Version)
		return 0
	case "cache":
		return cacheCommand(arguments[1:])
	case "ai":
		// AI setup is intentionally user-local and does not require a Git
		// repository. The advisory explain command is dispatched below after
		// resolving the repository it needs for staged deterministic evidence.
		if len(arguments) > 1 && arguments[1] == "setup" {
			return aiSetupCommand(arguments[2:])
		}
	}
	root, err := gitio.RepoRoot(mustWorkingDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "PhantomGuard:", err)
		return 2
	}
	switch arguments[0] {
	case "verify":
		return verifyCommand(root, arguments[1:])
	case "scan":
		return scanCommand(root, arguments[1:])
	case "tui":
		return tuiCommand(root, arguments[1:])
	case "install":
		return installCommand(root, arguments[1:])
	case "fix":
		return fixCommand(root, arguments[1:])
	case "ai":
		return aiCommand(root, arguments[1:])
	default:
		fmt.Fprintf(os.Stderr, "PhantomGuard: unknown command %q\n", arguments[0])
		usage(os.Stderr)
		return 2
	}
}

func scanCommand(root string, arguments []string) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	staged := flags.Bool("staged", false, "scan staged Git-index content")
	all := flags.Bool("all", false, "scan all supported working-tree files")
	strict := flags.Bool("strict", false, "block unknown registry results")
	asJSON := flags.Bool("json", false, "render a JSON report")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *staged && (*all || flags.NArg() > 0) || *all && flags.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: choose exactly one of --staged, --all, or paths")
		return 2
	}
	if !*staged && !*all && flags.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: choose --staged, --all, or paths")
		return 2
	}
	if os.Getenv("PHANTOMGUARD_SKIP") == "1" {
		fmt.Println("PhantomGuard skipped because PHANTOMGUARD_SKIP=1")
		return 0
	}
	var cfg config.Config
	var err error
	if *staged {
		cfg, err = config.LoadIndex(root, *strict)
	} else {
		cfg, err = config.Load(root, *strict)
	}
	if err != nil {
		return commandError(err, "configuration")
	}
	cache, err := validator.NewCache("")
	if err != nil {
		return commandError(err, "cache")
	}
	var contents map[string]string
	var evidence map[string]string
	var indexPaths map[string]bool
	var notices []string
	modulePrefix := ""
	if *staged {
		contents, notices, modulePrefix, err = stagedContents(root, cfg.Ignore)
		if err == nil {
			var evidenceNotices []string
			evidence, indexPaths, evidenceNotices, err = stagedIndexEvidence(root, contents)
			notices = append(notices, evidenceNotices...)
		}
	} else {
		contents, notices, modulePrefix, err = workingContents(root, flags.Args(), *all, cfg.Ignore)
	}
	if err != nil {
		return commandError(err, "scan inputs")
	}
	if evidence == nil {
		evidence = contents
	}
	checkerConfig := cfg
	if !*staged && !*all {
		// Explicit paths are a maintainer's direct request to inspect a file.
		// They intentionally override automatic ignore patterns, which keeps
		// unsafe demo fixtures inspectable without changing verify.
		checkerConfig.Ignore = nil
	}
	checker := scanner.New(root, checkerConfig, validator.NewClient(), cache)
	var scan model.Report
	if *staged {
		scan, err = checker.ScanIndexContents(context.Background(), contents, evidence, indexPaths, modulePrefix)
	} else {
		scan, err = checker.ScanContentsWithEvidence(context.Background(), contents, evidence, modulePrefix)
	}
	if err != nil {
		return scanFailure(err, cfg.FailMode)
	}
	scan.Notices = append(scan.Notices, notices...)
	scan.AnalysisIncomplete = append(scan.AnalysisIncomplete, scanner.IncompleteNotices(notices)...)
	output, err := report.Render(scan, cfg.FailMode, *asJSON)
	if err != nil {
		return scanFailure(err, cfg.FailMode)
	}
	fmt.Print(output)
	if report.Blocked(scan, cfg.FailMode) {
		return 1
	}
	return 0
}

// verifyCommand is the deterministic, staged-only enforcement plane. It never
// loads the optional AI configuration or calls an AI provider.
func verifyCommand(root string, arguments []string) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	strict := flags.Bool("strict", false, "block unknown registry results and weak provenance")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: verify accepts flags only")
		return 2
	}
	scan, cfg, err := stagedDeterministicReport(context.Background(), root, *strict)
	if err != nil {
		return commandError(err, "verify")
	}
	output, err := report.Render(scan, cfg.FailMode, false)
	if err != nil {
		return scanFailure(err, cfg.FailMode)
	}
	fmt.Print(output)
	if report.Blocked(scan, cfg.FailMode) {
		return 1
	}
	return 0
}

// stagedDeterministicReport supplies staged registry evidence to both verify
// and the separately invoked AI advisory command. It has no AI dependency.
func stagedDeterministicReport(ctx context.Context, root string, forceStrict bool) (model.Report, config.Config, error) {
	cfg, err := config.LoadIndex(root, forceStrict)
	if err != nil {
		return model.Report{}, cfg, fmt.Errorf("configuration: %w", err)
	}
	cache, err := validator.NewCache("")
	if err != nil {
		return model.Report{}, cfg, fmt.Errorf("cache: %w", err)
	}
	contents, notices, incomplete, modulePrefix, err := indexCandidateContents(root, cfg.Ignore)
	if err != nil {
		return model.Report{}, cfg, fmt.Errorf("scan inputs: %w", err)
	}
	evidence, indexPaths, evidenceNotices, err := stagedIndexEvidence(root, contents)
	if err != nil {
		return model.Report{}, cfg, fmt.Errorf("provenance inputs: %w", err)
	}
	notices = append(notices, evidenceNotices...)
	checker := scanner.New(root, cfg, validator.NewClient(), cache)
	scan, err := checker.ScanIndexContents(ctx, contents, evidence, indexPaths, modulePrefix)
	if err != nil {
		return scan, cfg, err
	}
	scan.Notices = append(scan.Notices, notices...)
	scan.AnalysisIncomplete = append(scan.AnalysisIncomplete, incomplete...)
	return scan, cfg, nil
}

// indexCandidateContents reads every supported text file from the current Git
// index. Unlike the manual `scan --staged` view, verification must evaluate
// the complete staged snapshot so deleting a lockfile or policy cannot weaken
// provenance for unchanged source code.
func indexCandidateContents(root string, ignores []string) (map[string]string, []string, []string, string, error) {
	contents := make(map[string]string)
	var notices []string
	var incomplete []string
	paths, err := gitio.IndexFiles(root)
	if err != nil {
		return nil, nil, nil, "", err
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
			return nil, nil, nil, "", err
		}
		if len(raw) > maxFileSize {
			reason := path + ": skipped (>1 MiB)"
			notices = append(notices, reason)
			incomplete = append(incomplete, reason)
			continue
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			reason := path + ": skipped (binary or non-UTF-8 staged content)"
			notices = append(notices, reason)
			incomplete = append(incomplete, reason)
			continue
		}
		contents[path] = content
	}
	modulePrefix := ""
	if raw, err := gitio.StagedContent(root, "go.mod"); err == nil {
		if content, ok := gitio.DecodeUTF8(raw); ok {
			modulePrefix = extractor.ModulePath(content)
		}
	}
	return contents, notices, incomplete, modulePrefix, nil
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
		if len(raw) > maxFileSize {
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
	modulePrefix := ""
	if raw, err := gitio.StagedContent(root, "go.mod"); err == nil {
		if content, ok := gitio.DecodeUTF8(raw); ok {
			modulePrefix = extractor.ModulePath(content)
		}
	}
	return contents, notices, modulePrefix, nil
}

func workingContents(root string, selected []string, all bool, ignores []string) (map[string]string, []string, string, error) {
	contents := make(map[string]string)
	var notices []string
	files := make([]string, 0)
	if all {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				relative, relativeErr := filepath.Rel(root, path)
				if relativeErr != nil {
					relative = path
				}
				notices = append(notices, filepath.ToSlash(relative)+": skipped ("+err.Error()+")")
				return nil
			}
			if entry.IsDir() {
				if isToolCacheDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				relative, relativeErr := filepath.Rel(root, path)
				if relativeErr != nil {
					return relativeErr
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
			files = append(files, relative)
			return nil
		})
		if err != nil {
			return nil, nil, "", err
		}
	} else {
		files = selected
	}
	for _, relative := range files {
		relative = filepath.Clean(relative)
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, "", fmt.Errorf("path escapes repository: %s", relative)
		}
		if !extractor.Relevant(relative) {
			continue
		}
		// A selected path is an explicit user request, so it deliberately
		// overrides automatic ignore patterns. This lets maintainers inspect
		// documented fixtures such as demo files without weakening verify.
		if all && scanner.Ignored(relative, ignores) {
			notices = append(notices, filepath.ToSlash(relative)+": ignored by .phantomguard.json")
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				notices = append(notices, relative+": skipped (not found)")
				continue
			}
			return nil, nil, "", err
		}
		if len(raw) > maxFileSize {
			notices = append(notices, relative+": skipped (>1 MiB)")
			continue
		}
		content, ok := gitio.DecodeUTF8(raw)
		if !ok {
			notices = append(notices, relative+": skipped (binary or non-UTF-8 content)")
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

func isToolCacheDirectory(name string) bool {
	switch name {
	case ".git", ".pytest_cache", ".ruff_cache", "node_modules", ".mypy_cache", ".tox", "bin", "dist":
		return true
	default:
		return false
	}
}

func installCommand(root string, arguments []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	force := flags.Bool("force", false, "back up and chain a foreign hook")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: install accepts no positional arguments")
		return 2
	}
	hook, err := gitio.InstallHook(root, *force)
	if err != nil {
		return commandError(err, "install")
	}
	fmt.Println("Installed PhantomGuard hook at", hook)
	return 0
}

func cacheCommand(arguments []string) int {
	cache, err := validator.NewCache("")
	if err != nil {
		return commandError(err, "cache")
	}
	switch {
	case len(arguments) == 0 || len(arguments) == 1 && arguments[0] == "status":
		stats := cache.Stats()
		fmt.Printf("PhantomGuard cache: %d definitive result(s) (%d exists, %d phantom)\n", stats.Entries, stats.Exists, stats.Phantom)
		return 0
	case len(arguments) == 1 && arguments[0] == "clear":
		if err := cache.Clear(); err != nil {
			return commandError(err, "cache")
		}
		fmt.Println("PhantomGuard cache cleared")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "PhantomGuard: use cache, cache status, or cache clear")
		return 2
	}
}

func fixCommand(root string, arguments []string) int {
	flags := flag.NewFlagSet("fix", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	path := flags.String("file", "", "repository-relative file to edit")
	from := flags.String("from", "", "phantom package string to replace")
	to := flags.String("to", "", "registry-verified replacement")
	ecosystem := flags.String("ecosystem", "", "pypi or npm")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "PhantomGuard: fix accepts flags only")
		return 2
	}
	if err := remediation.PreviewAndApply(context.Background(), root, remediation.Fix{Path: *path, From: *from, To: *to, Ecosystem: model.Ecosystem(strings.ToLower(*ecosystem))}, validator.NewClient(), os.Stdin, os.Stdout); err != nil {
		return commandError(err, "fix")
	}
	return 0
}

func scanFailure(err error, failMode string) int {
	fmt.Fprintln(os.Stderr, "PhantomGuard scan error:", err)
	if failMode == "strict" {
		return 1
	}
	return 0
}
func commandError(err error, operation string) int {
	fmt.Fprintf(os.Stderr, "PhantomGuard %s error: %v\n", operation, err)
	return 2
}
func mustWorkingDir() string {
	current, err := os.Getwd()
	if err != nil {
		return "."
	}
	return current
}
func usage(output *os.File) {
	fmt.Fprintln(output, "Usage: phantomguard <verify|scan|tui|install|cache|fix|ai|version> [options]")
	fmt.Fprintln(output, "Verify: phantomguard verify --strict  # deterministic staged enforcement")
	fmt.Fprintln(output, "AI:     phantomguard ai setup | phantomguard ai explain <package> [--ecosystem pypi|npm]")
}

// stagedIndexEvidence records every current Git-index path separately from
// the manifests needed for provenance and local-package filtering. Its text
// may substantiate a staged finding, but is excluded from candidate extraction.
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
		if len(raw) > maxFileSize {
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
