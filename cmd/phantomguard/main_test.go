package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
	"github.com/phantomguard/phantomguard/pkg/config"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/report"
	"github.com/phantomguard/phantomguard/pkg/scanner"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

func TestHelpWorksOutsideGitRepository(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change to temporary directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	if exitCode := run([]string{"--help"}); exitCode != 0 {
		t.Fatalf("help exit code = %d, want 0", exitCode)
	}
}

func TestVersionWorksOutsideGitRepository(t *testing.T) {
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change to temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = originalStdout })
	if exitCode := run([]string{"--version"}); exitCode != 0 {
		t.Fatalf("version exit code = %d, want 0", exitCode)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	var output bytes.Buffer
	if _, err := output.ReadFrom(reader); err != nil {
		t.Fatalf("read version output: %v", err)
	}
	if got := output.String(); got != buildinfo.Version+"\n" {
		t.Fatalf("version output = %q, want %q", got, buildinfo.Version+"\n")
	}
}

func TestCacheStatusAliasesWorkOutsideGitRepository(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = originalStdout })
	for _, arguments := range [][]string{{"cache"}, {"cache", "status"}} {
		if exitCode := run(arguments); exitCode != 0 {
			t.Fatalf("%v exit code = %d, want 0", arguments, exitCode)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	var output bytes.Buffer
	if _, err := output.ReadFrom(reader); err != nil {
		t.Fatalf("read cache output: %v", err)
	}
	if strings.Count(output.String(), "PhantomGuard cache: 0 definitive result(s) (0 exists, 0 phantom)") != 2 {
		t.Fatalf("cache status output = %q", output.String())
	}
}

func TestSelectedCLIPathOverridesAutomaticIgnorePolicy(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "demo", "fixture.py")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("import phantom_fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, notices, _, err := workingContents(root, []string{"demo/fixture.py"}, false, []string{"demo/**"})
	if err != nil {
		t.Fatalf("read selected fixture: %v", err)
	}
	if got := contents["demo/fixture.py"]; got != "import phantom_fixture\n" {
		t.Fatalf("selected ignored fixture = %q, want source content", got)
	}
	if len(notices) != 0 {
		t.Fatalf("selected fixture notices = %#v, want none", notices)
	}
}

func TestAutomaticCLIAllScanReportsIgnoredFixtureDirectory(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "demo", "fixture.py")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("import phantom_fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, notices, _, err := workingContents(root, nil, true, []string{"demo/**"})
	if err != nil {
		t.Fatalf("read automatic scan: %v", err)
	}
	if len(contents) != 0 {
		t.Fatalf("automatic scan included ignored fixture: %#v", contents)
	}
	if len(notices) != 1 || notices[0] != "demo/: ignored by .phantomguard.json" {
		t.Fatalf("automatic ignored notices = %#v", notices)
	}
}

func TestIndexVerificationRescansUnchangedSourcesAfterProvenanceDeletion(t *testing.T) {
	for _, test := range []struct {
		name           string
		sourcePath     string
		provenancePath string
		files          map[string]string
	}{
		{
			name:           "Python requirements",
			sourcePath:     "app.py",
			provenancePath: "requirements.txt",
			files: map[string]string{
				"app.py":           "import requests\n",
				"requirements.txt": "requests==2.31.0 --hash=sha256:abc\n",
			},
		},
		{
			name:           "npm lockfile",
			sourcePath:     "app.js",
			provenancePath: "package-lock.json",
			files: map[string]string{
				"app.js":            "require('left-pad')\n",
				"package.json":      `{"dependencies":{"left-pad":"1.3.0"}}`,
				"package-lock.json": `{"packages":{"node_modules/left-pad":{"version":"1.3.0","integrity":"sha512-abc"}}}`,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			runGit(t, root, "init", "-q")
			runGit(t, root, "config", "user.email", "phantomguard-test@example.test")
			runGit(t, root, "config", "user.name", "PhantomGuard Test")
			for path, content := range test.files {
				fixturePath := filepath.Join(root, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(fixturePath), 0o700); err != nil {
					t.Fatalf("create fixture directory: %v", err)
				}
				if err := os.WriteFile(fixturePath, []byte(content), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
			}
			runGit(t, root, "add", ".")
			runGit(t, root, "commit", "-m", "seed provenance")
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(test.provenancePath))); err != nil {
				t.Fatalf("delete staged provenance fixture: %v", err)
			}
			runGit(t, root, "add", "-u")

			contents, notices, incomplete, modulePrefix, err := indexCandidateContents(root, nil)
			if err != nil {
				t.Fatalf("read index candidates: %v", err)
			}
			if _, ok := contents[test.sourcePath]; !ok {
				t.Fatalf("unchanged source %q was not rescanned: %#v", test.sourcePath, contents)
			}
			if _, ok := contents[test.provenancePath]; ok {
				t.Fatalf("deleted provenance %q remained in index candidates", test.provenancePath)
			}
			evidence, indexPaths, evidenceNotices, err := stagedIndexEvidence(root, contents)
			if err != nil {
				t.Fatalf("read index evidence: %v", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.FailMode = "strict"
			client := validator.NewClient()
			client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
			verification := scanner.New(root, cfg, client, cache)
			scan, err := verification.ScanIndexContents(context.Background(), contents, evidence, indexPaths, modulePrefix)
			if err != nil {
				t.Fatal(err)
			}
			scan.Notices = append(scan.Notices, notices...)
			scan.Notices = append(scan.Notices, evidenceNotices...)
			scan.AnalysisIncomplete = append(scan.AnalysisIncomplete, incomplete...)
			if !report.Blocked(scan, cfg.FailMode) {
				t.Fatalf("strict verification allowed a deleted provenance file: %#v", scan)
			}
		})
	}
}

func TestIndexVerificationMarksOversizedCandidatesIncomplete(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	contents := "import requests\n" + strings.Repeat("# padding\n", maxFileSize/8+1)
	if err := os.WriteFile(filepath.Join(root, "large.py"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "large.py")
	_, _, incomplete, _, err := indexCandidateContents(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 1 || !strings.Contains(incomplete[0], "large.py: skipped (>1 MiB)") {
		t.Fatalf("oversized candidate was not recorded as incomplete: %#v", incomplete)
	}
	if !report.Blocked(model.Report{AnalysisIncomplete: incomplete}, "strict") {
		t.Fatal("strict verification allowed an oversized candidate")
	}
}

func TestStrictManualStagedScanBlocksSkippedCandidate(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	contents := "import requests\n" + strings.Repeat("# padding\n", maxFileSize/8+1)
	if err := os.WriteFile(filepath.Join(root, "large.py"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "large.py")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if exitCode := scanCommand(root, []string{"--staged", "--strict"}); exitCode != 1 {
		t.Fatalf("strict staged scan exit code = %d, want 1", exitCode)
	}
}

func TestParseAIExplainArgumentsAcceptsDocumentedFlagOrder(t *testing.T) {
	for _, arguments := range [][]string{
		{"reqeusts", "--ecosystem", "pypi"},
		{"--ecosystem=pypi", "reqeusts"},
	} {
		packageName, ecosystem, err := parseAIExplainArguments(arguments)
		if err != nil || packageName != "reqeusts" || ecosystem != model.PyPI {
			t.Fatalf("parse %v = %q, %q, %v", arguments, packageName, ecosystem, err)
		}
	}
	if _, _, err := parseAIExplainArguments([]string{"one", "two"}); err == nil {
		t.Fatal("multiple AI explain package names were accepted")
	}
}

func TestSelectAIExplainFindingDeduplicatesRepeatedPackageInOneEcosystem(t *testing.T) {
	scan := model.Report{Results: []model.Result{
		{Finding: model.Finding{Name: "reqeusts", Ecosystem: model.PyPI, Path: "one.py", Line: 1}, Package: "reqeusts", Status: model.Phantom},
		{Finding: model.Finding{Name: "reqeusts", Ecosystem: model.PyPI, Path: "two.py", Line: 8}, Package: "reqeusts", Status: model.Phantom},
	}}
	finding, err := selectAIExplainFinding(scan, "reqeusts", "")
	if err != nil {
		t.Fatalf("select repeated finding: %v", err)
	}
	if finding.Finding.Path != "one.py" || finding.Finding.Ecosystem != model.PyPI {
		t.Fatalf("selected finding = %#v, want first PyPI finding", finding)
	}
}

func TestSelectAIExplainFindingRejectsCrossEcosystemAmbiguity(t *testing.T) {
	scan := model.Report{Results: []model.Result{
		{Finding: model.Finding{Name: "phantom", Ecosystem: model.PyPI}, Package: "phantom", Status: model.Phantom},
		{Finding: model.Finding{Name: "phantom", Ecosystem: model.NPM}, Package: "phantom", Status: model.Phantom},
	}}
	if _, err := selectAIExplainFinding(scan, "phantom", ""); err == nil {
		t.Fatal("cross-ecosystem package ambiguity was accepted")
	}
	finding, err := selectAIExplainFinding(scan, "phantom", model.NPM)
	if err != nil || finding.Finding.Ecosystem != model.NPM {
		t.Fatalf("ecosystem-qualified selection = %#v, %v", finding, err)
	}
}

func TestAISetupWorksOutsideGitRepositoryAndRejectsArguments(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change to temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	if exitCode := run([]string{"ai", "setup", "unexpected"}); exitCode != 2 {
		t.Fatalf("ai setup invalid invocation exit code = %d, want 2", exitCode)
	}
}
