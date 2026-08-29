// update_top_packages refreshes PhantomGuard's build-time popularity data; it is never called by the hook.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultPyPIURL = "https://hugovk.dev/top-pypi-packages/top-pypi-packages.min.json"
	defaultNPMURL  = "https://gist.githubusercontent.com/anvaka/8e8fa57c7ee1350e3491/raw/01.most-dependent-upon.md"
)

var (
	pypiName    = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9._-]*$`)
	npmName     = regexp.MustCompile(`(?i)^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
	npmMarkdown = regexp.MustCompile(`(?m)^\s*\d+\.\s+\[([^\]]+)\]\([^\)]*\)\s+-`)
)

func main() {
	limit := flag.Int("limit", 1000, "number of packages to retain per ecosystem")
	pypiURL := flag.String("pypi-url", defaultPyPIURL, "PyPI popularity JSON URL")
	npmURL := flag.String("npm-url", defaultNPMURL, "npm popularity Markdown URL")
	outputDir := flag.String("output-dir", "data", "directory containing top_pypi.txt and top_npm.txt")
	write := flag.Bool("write", false, "atomically replace the corpus files after review")
	flag.Parse()
	if *limit < 1 {
		fatal("limit must be positive")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	pypi, err := fetchPyPI(client, *pypiURL, *limit)
	if err != nil {
		fatal(err.Error())
	}
	npm, err := fetchNPM(client, *npmURL, *limit)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Printf("Fetched and validated %d PyPI names and %d npm names.\n", len(pypi), len(npm))
	if !*write {
		fmt.Println("Review the source and rerun with --write to replace the versioned corpus files.")
		return
	}
	if err := atomicWrite(filepath.Join(*outputDir, "top_pypi.txt"), pypi); err != nil {
		fatal(err.Error())
	}
	if err := atomicWrite(filepath.Join(*outputDir, "top_npm.txt"), npm); err != nil {
		fatal(err.Error())
	}
	fmt.Println("Updated corpus files. Review the Git diff before committing.")
}

func fetchPyPI(client *http.Client, source string, limit int) ([]string, error) {
	raw, err := fetch(client, source)
	if err != nil {
		return nil, err
	}
	var feed struct {
		Rows []struct {
			Project string `json:"project"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("decode PyPI feed: %w", err)
	}
	names := make([]string, 0, limit)
	for _, row := range feed.Rows {
		if len(names) == limit {
			break
		}
		names = append(names, strings.ToLower(strings.TrimSpace(row.Project)))
	}
	return validate(names, limit, pypiName, "PyPI")
}

func fetchNPM(client *http.Client, source string, limit int) ([]string, error) {
	raw, err := fetch(client, source)
	if err != nil {
		return nil, err
	}
	matches := npmMarkdown.FindAllSubmatch(raw, -1)
	names := make([]string, 0, limit)
	for _, match := range matches {
		if len(names) == limit {
			break
		}
		names = append(names, strings.ToLower(strings.TrimSpace(string(match[1]))))
	}
	return validate(names, limit, npmName, "npm")
}

func fetch(client *http.Client, source string) ([]byte, error) {
	response, err := client.Get(source)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected HTTP %s", source, response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	return raw, nil
}

func validate(names []string, limit int, valid *regexp.Regexp, ecosystem string) ([]string, error) {
	if len(names) != limit {
		return nil, fmt.Errorf("%s feed yielded %d names, expected %d", ecosystem, len(names), limit)
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !valid.MatchString(name) {
			return nil, fmt.Errorf("%s feed returned an invalid package token %q", ecosystem, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s feed returned duplicate package %q", ecosystem, name)
		}
		seen[name] = true
	}
	return names, nil
}

func atomicWrite(path string, names []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "top-packages-*.txt")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.WriteString(strings.Join(names, "\n") + "\n"); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "update_top_packages:", message)
	os.Exit(1)
}
