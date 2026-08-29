// Package remediation provides the deliberately interactive PhantomGuard safe auto-fixer.
package remediation

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/phantomguard/phantomguard/pkg/gitio"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

// Fix describes one approved replacement in a working-tree file.
type Fix struct {
	Path      string
	From      string
	To        string
	Ecosystem model.Ecosystem
}

// PreviewAndApply proves the replacement exists, shows a unified diff, requires y confirmation, then revalidates.
func PreviewAndApply(ctx context.Context, root string, fix Fix, registry *validator.Client, input io.Reader, output io.Writer) error {
	if fix.From == "" || fix.To == "" {
		return fmt.Errorf("--from and --to are required")
	}
	if fix.Ecosystem != model.PyPI && fix.Ecosystem != model.NPM {
		return fmt.Errorf("fix supports only pypi or npm packages")
	}
	if err := registry.VerifyExists(ctx, fix.Ecosystem, fix.To); err != nil {
		return fmt.Errorf("refusing unverified replacement: %w", err)
	}
	changed, err := gitio.HasUnstagedChanges(root)
	if err != nil {
		return err
	}
	if changed {
		return fmt.Errorf("refusing to edit while git diff reports unstaged changes")
	}
	path, err := safePath(root, fix.Path)
	if err != nil {
		return err
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", fix.Path, err)
	}
	if !utf8.Valid(beforeBytes) {
		return fmt.Errorf("refusing to edit non-UTF-8 file %s", fix.Path)
	}
	before := string(beforeBytes)
	if !strings.Contains(before, fix.From) {
		return fmt.Errorf("%q does not occur in %s", fix.From, fix.Path)
	}
	after := strings.ReplaceAll(before, fix.From, fix.To)
	fmt.Fprint(output, unifiedDiff(fix.Path, before, after))
	fmt.Fprint(output, "Apply this verified replacement? [y/N] ")
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		fmt.Fprintln(output, "No files were changed.")
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", fix.Path, err)
	}
	if err := os.WriteFile(path, []byte(after), info.Mode()); err != nil {
		return fmt.Errorf("write %s: %w", fix.Path, err)
	}
	if err := registry.VerifyExists(ctx, fix.Ecosystem, fix.To); err != nil {
		if restoreErr := os.WriteFile(path, beforeBytes, info.Mode()); restoreErr != nil {
			return fmt.Errorf("post-fix validation failed (%v) and restore failed (%v)", err, restoreErr)
		}
		return fmt.Errorf("post-fix validation failed; restored original file: %w", err)
	}
	fmt.Fprintln(output, "Applied and re-validated the replacement.")
	return nil
}

func safePath(root, supplied string) (string, error) {
	if filepath.IsAbs(supplied) {
		return "", fmt.Errorf("fix path must be repository-relative")
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	rootInfo, err := os.Lstat(rootPath)
	if err != nil {
		return "", fmt.Errorf("inspect repository root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to edit through a symbolic-link repository root")
	}
	path := filepath.Join(rootPath, filepath.Clean(supplied))
	relative, err := filepath.Rel(rootPath, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("fix path escapes the repository")
	}
	// Reject symbolic links in every component. A lexical relative path can
	// otherwise traverse a tracked symlink into a file outside the repository.
	current := rootPath
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect fix path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to edit through symbolic link %s", supplied)
		}
	}
	return path, nil
}

func unifiedDiff(path, before, after string) string {
	beforeLines, afterLines := strings.Split(before, "\n"), strings.Split(after, "\n")
	var output strings.Builder
	fmt.Fprintf(&output, "--- a/%s\n+++ b/%s\n", filepath.ToSlash(path), filepath.ToSlash(path))
	for index := 0; index < len(beforeLines) || index < len(afterLines); index++ {
		var oldLine, newLine string
		if index < len(beforeLines) {
			oldLine = beforeLines[index]
		}
		if index < len(afterLines) {
			newLine = afterLines[index]
		}
		if oldLine == newLine {
			continue
		}
		fmt.Fprintf(&output, "@@ -%d +%d @@\n-%s\n+%s\n", index+1, index+1, oldLine, newLine)
	}
	return output.String()
}
