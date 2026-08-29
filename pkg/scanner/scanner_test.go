package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantomguard/phantomguard/pkg/config"
	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/report"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

func TestScanReturnsPhantomAndNeverQueriesPythonStdlib(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path == "/pypi/ghost-package/json" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	scanner := New(t.TempDir(), config.Default(), client, cache)
	report, err := scanner.ScanContents(context.Background(), map[string]string{"app.py": "import os\nimport ghost_package\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != model.Phantom || requests != 1 {
		t.Fatalf("report=%#v requests=%d", report, requests)
	}
}

func TestIgnoredSupportsDoubleStar(t *testing.T) {
	if !Ignored("services/api/migrations/0001.py", []string{"**/migrations/**"}) {
		t.Fatal("migrations path was not ignored")
	}
}

func TestScanRendersHighRiskTyposquatWarningAndBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/reaxt" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(t.TempDir(), config.Default(), client, cache)
	scan, err := checker.ScanContents(context.Background(), map[string]string{"app.js": "require('reaxt')\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || scan.Results[0].Status != model.Phantom || scan.Results[0].RiskMatch != "react" {
		t.Fatalf("unexpected typosquat result: %#v", scan.Results)
	}
	output, err := report.Render(scan, "warn", false)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Blocked(scan, "warn") || !strings.Contains(output, "HIGH-RISK TYPOSQUAT: resembles react") {
		t.Fatalf("missing high-risk warning or block:\n%s", output)
	}
}

func TestScanAnnotatesHashPinnedPythonRequirementsProvenance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(t.TempDir(), config.Default(), client, cache)
	scan, err := checker.ScanContents(context.Background(), map[string]string{"requirements.txt": "requests==2.31.0 --hash=sha256:abc\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 {
		t.Fatalf("unexpected results: %#v", scan.Results)
	}
	if !strings.Contains(scan.Results[0].Provenance, "hash-pinned") {
		t.Fatalf("requirements provenance missing hash evidence: %#v", scan.Results[0])
	}
}

func TestScanUsesSeparateIndexProvenanceEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(t.TempDir(), config.Default(), client, cache)
	scan, err := checker.ScanContentsWithEvidence(context.Background(),
		map[string]string{"app.py": "import requests\n"},
		map[string]string{"app.py": "import requests\n", "requirements.txt": "requests==2.31.0 --hash=sha256:abc\n"},
		"")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || !scan.Results[0].ProvenanceVerified || !strings.Contains(scan.Results[0].Provenance, "hash-pinned") {
		t.Fatalf("separate provenance evidence was not applied: %#v", scan.Results)
	}
}

func TestProvenanceDoesNotCrossWorkspaceBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(t.TempDir(), config.Default(), client, mustTestCache(t))
	scan, err := checker.ScanContentsWithEvidence(context.Background(),
		map[string]string{"apps/b/app.py": "import requests\n"},
		map[string]string{
			"apps/a/requirements.txt": "requests==2.31.0 --hash=sha256:abc\n",
			"apps/b/app.py":           "import requests\n",
		},
		"")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || scan.Results[0].ProvenanceVerified {
		t.Fatalf("sibling workspace lockfile was accepted as provenance: %#v", scan.Results)
	}
}

func TestStrictModeBlocksIncompleteAnalysis(t *testing.T) {
	cfg := config.Default()
	cfg.FailMode = "strict"
	checker := New(t.TempDir(), cfg, validator.NewClient(), mustTestCache(t))
	for name, contents := range map[string]map[string]string{
		"invalid manifest": {"package.json": "{"},
		"dynamic import":   {"app.py": "import importlib\nimportlib.import_module(name)\n"},
	} {
		t.Run(name, func(t *testing.T) {
			scan, err := checker.ScanContents(context.Background(), contents, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(scan.AnalysisIncomplete) == 0 {
				t.Fatalf("incomplete analysis was not recorded: %#v", scan)
			}
			if !report.Blocked(scan, cfg.FailMode) {
				t.Fatalf("strict policy allowed incomplete analysis: %#v", scan)
			}
		})
	}
}

func TestScanIndexContentsIgnoresUntrackedLocalPythonModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evil.py"), []byte("# untracked working-tree bypass attempt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/pypi/evil/json" {
			t.Errorf("unexpected lookup: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(root, config.Default(), client, cache)
	scan, err := checker.ScanIndexContents(context.Background(),
		map[string]string{"app.py": "import evil\n"},
		map[string]string{"app.py": "import evil\n"},
		map[string]bool{"app.py": true},
		"")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || scan.Results[0].Status != model.Phantom || requests != 1 {
		t.Fatalf("index scan trusted the working tree: results=%#v requests=%d", scan.Results, requests)
	}
}

func TestIndexLocalFilteringUsesOnlyIndexEvidence(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"evil.py":                        "# working-tree only\n",
		"package.json":                   `{"workspaces":["packages/*"]}`,
		"packages/internal/package.json": `{"name":"internal-package"}`,
		"tsconfig.json":                  `{"compilerOptions":{"paths":{"local-alias":[]}}}`,
	} {
		filename := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checker := New(root, config.Default(), validator.NewClient(), mustTestCache(t))
	emptyEvidence := map[string]string{}
	emptyIndexPaths := map[string]bool{}
	for _, test := range []struct {
		finding model.Finding
		name    string
	}{
		{model.Finding{Ecosystem: model.PyPI, Name: "evil"}, "evil"},
		{model.Finding{Ecosystem: model.NPM, Name: "internal-package"}, "internal-package"},
		{model.Finding{Ecosystem: model.NPM, Name: "local-alias"}, "local-alias"},
	} {
		local, err := checker.localOrAllowed(test.finding, test.name, true, emptyEvidence, emptyIndexPaths)
		if err != nil || local {
			t.Fatalf("index filter consulted working tree for %s: local=%t err=%v", test.name, local, err)
		}
	}
	evidence := map[string]string{
		"package.json":                   `{"workspaces":["packages/*"]}`,
		"packages/internal/package.json": `{"name":"internal-package"}`,
		"tsconfig.json":                  `{"compilerOptions":{"paths":{"local-alias":[]}}}`,
	}
	indexPaths := map[string]bool{
		"evil.py":                        true,
		"package.json":                   true,
		"packages/internal/package.json": true,
		"tsconfig.json":                  true,
	}
	for _, test := range []struct {
		finding model.Finding
		name    string
	}{
		{model.Finding{Ecosystem: model.PyPI, Name: "evil"}, "evil"},
		{model.Finding{Ecosystem: model.NPM, Name: "internal-package"}, "internal-package"},
		{model.Finding{Ecosystem: model.NPM, Name: "local-alias"}, "local-alias"},
	} {
		local, err := checker.localOrAllowed(test.finding, test.name, true, evidence, indexPaths)
		if err != nil || !local {
			t.Fatalf("index filter missed tracked local package %s: local=%t err=%v", test.name, local, err)
		}
	}
}

func mustTestCache(t *testing.T) *validator.Cache {
	t.Helper()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestScanAnnotatesNPMLockfileProvenance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	checker := New(t.TempDir(), config.Default(), client, cache)
	contents := map[string]string{
		"package.json":      `{"dependencies":{"left-pad":"1.3.0"}}`,
		"package-lock.json": `{"packages":{"node_modules/left-pad":{"version":"1.3.0","integrity":"sha512-abc"}}}`,
	}
	scan, err := checker.ScanContents(context.Background(), contents, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 {
		t.Fatalf("unexpected results: %#v", scan.Results)
	}
	if !strings.Contains(scan.Results[0].Provenance, "integrity-backed") {
		t.Fatalf("npm provenance missing integrity evidence: %#v", scan.Results[0])
	}
}

func TestScanAnnotatesGoSumBackedProvenance(t *testing.T) {
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	checker := New(t.TempDir(), config.Default(), client, cache)
	contents := map[string]string{
		"go.mod":  "module example.com/repo\n\nrequire github.com/pkg/errors v0.9.1\n",
		"go.sum":  "github.com/pkg/errors v0.9.1 h1:abc\ngithub.com/pkg/errors v0.9.1/go.mod h1:def\n",
		"main.go": "package main\nimport \"github.com/pkg/errors\"\n",
	}
	scan, err := checker.ScanContents(context.Background(), contents, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 2 {
		t.Fatalf("unexpected results: %#v", scan.Results)
	}
	for _, result := range scan.Results {
		if result.Status != model.Exists || !result.ProvenanceVerified {
			t.Fatalf("checksum-backed Go module was not verified: %#v", result)
		}
		if !strings.Contains(result.Provenance, "go.sum-backed") {
			t.Fatalf("go provenance missing checksum evidence: %#v", result)
		}
	}
	if report.Blocked(scan, "strict") {
		t.Fatalf("strict policy blocked checksum-backed Go modules: %#v", scan)
	}
}

func TestGoModOnlyChecksumDoesNotVerifyModuleContent(t *testing.T) {
	checker := New(t.TempDir(), config.Default(), validator.NewClient(), mustTestCache(t))
	scan, err := checker.ScanContents(context.Background(), map[string]string{
		"go.mod": "module example.com/repo\n\nrequire github.com/pkg/errors v0.9.1\n",
		"go.sum": "github.com/pkg/errors v0.9.1/go.mod h1:def\n",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || scan.Results[0].Status != model.Unknown || scan.Results[0].ProvenanceVerified {
		t.Fatalf("go.mod-only checksum was treated as module verification: %#v", scan.Results)
	}
}

func TestNestedGoModuleImportsAreNotPublicCandidates(t *testing.T) {
	checker := New(t.TempDir(), config.Default(), validator.NewClient(), mustTestCache(t))
	contents := map[string]string{
		"go.mod":            "module github.com/example/root\n",
		"apps/tool/go.mod":  "module github.com/example/tool\n",
		"apps/tool/main.go": "package main\nimport \"github.com/example/tool/internal/run\"\n",
	}
	scan, err := checker.ScanContents(context.Background(), contents, "github.com/example/root/")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 0 {
		t.Fatalf("nested module's local import became a public candidate: %#v", scan.Results)
	}
}

func TestStrictModeBlocksWeakProvenance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cache, err := validator.NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL + "/pypi", NPM: server.URL}
	cfg := config.Default()
	cfg.FailMode = "strict"
	checker := New(t.TempDir(), cfg, client, cache)
	scan, err := checker.ScanContents(context.Background(), map[string]string{"requirements.txt": "requests==2.31.0\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Results) != 1 || scan.Results[0].Status != model.Exists || scan.Results[0].ProvenanceVerified {
		t.Fatalf("unexpected weak provenance scan result: %#v", scan.Results[0])
	}
	if !report.Blocked(scan, "strict") {
		t.Fatal("strict mode did not block weak provenance evidence")
	}
}
