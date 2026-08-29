// Package gitio safely reads Git metadata and the staged index without a shell.
package gitio

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"
)

func command(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	return cmd
}

// RepoRoot returns Git's canonical root directory.
func RepoRoot(cwd string) (string, error) {
	output, err := command(cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a Git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

// StagedFiles lists only index entries Git will create, modify, copy, or rename.
func StagedFiles(root string) ([]string, error) {
	output, err := command(root, "diff", "--cached", "--name-only", "--diff-filter=ACMR").Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}
	var paths []string
	for _, path := range strings.Split(string(output), "\n") {
		path = strings.TrimSuffix(path, "\r")
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// IndexFiles lists every path represented by the current Git index. The
// NUL-delimited form preserves unusual but valid file names exactly.
func IndexFiles(root string) ([]string, error) {
	output, err := command(root, "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("list index files: %w", err)
	}
	var paths []string
	for _, path := range strings.Split(string(output), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// StagedContent reads the exact blob from the Git index. Path is one argv item, never a shell string.
func StagedContent(root, path string) ([]byte, error) {
	output, err := command(root, "show", ":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("read staged %q: %w", path, err)
	}
	return output, nil
}

// DecodeUTF8 accepts only text that is valid UTF-8 and free of NUL bytes.
func DecodeUTF8(raw []byte) (string, bool) {
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		return "", false
	}
	return string(raw), true
}

// HasUnstagedChanges is true only when Git reports a working-tree change.
func HasUnstagedChanges(root string) (bool, error) {
	err := command(root, "diff", "--quiet").Run()
	if err == nil {
		return false, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("check unstaged changes: %w", err)
}
