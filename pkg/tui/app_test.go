package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

type testBackend struct {
	scans         []ScanRequest
	cacheClears   int
	installForces []bool
	fixes         []FixRequest
}

func (b *testBackend) Status(context.Context) (Status, error) {
	return Status{Repository: "demo-repo", Branch: "main", FailMode: "warn"}, nil
}

func (b *testBackend) Scan(_ context.Context, request ScanRequest) (ScanResult, error) {
	b.scans = append(b.scans, request)
	return ScanResult{
		Report:   model.Report{Results: []model.Result{}},
		FailMode: "warn",
	}, nil
}

func (b *testBackend) CacheStatus(context.Context) (CacheStatus, error) {
	return CacheStatus{Entries: 2, Verified: 1, Phantom: 1, PositiveTTLHours: 168, NegativeTTLHours: 1}, nil
}

func (b *testBackend) ClearCache(context.Context) error {
	b.cacheClears++
	return nil
}

func (b *testBackend) Install(_ context.Context, force bool) (string, error) {
	b.installForces = append(b.installForces, force)
	return ".git/hooks/pre-commit", nil
}

func (b *testBackend) Fix(_ context.Context, request FixRequest, _ io.Reader, output io.Writer) error {
	b.fixes = append(b.fixes, request)
	_, err := fmt.Fprintln(output, "Applied and re-validated the replacement.")
	return err
}

func TestRunConnectsPromptToBackend(t *testing.T) {
	backend := &testBackend{}
	interactive := true
	var output bytes.Buffer
	exitCode := Run(Options{
		Args:        []string{"--no-color"},
		In:          strings.NewReader("status\nscan staged\nscan app.py --strict\ncache\ncache clear\ninstall --force\nfix --file app.py --from reqeusts --to requests --ecosystem pypi\nquit\n"),
		Out:         &output,
		Err:         &bytes.Buffer{},
		Width:       80,
		Interactive: &interactive,
		Backend:     backend,
		Version:     "vtest",
		Build:       "test",
	})
	if exitCode != 0 {
		t.Fatalf("Run exit code = %d, want 0", exitCode)
	}
	if len(backend.scans) != 2 || backend.scans[0].Target != TargetStaged || backend.scans[1].Target != TargetPaths || !backend.scans[1].Strict || !reflect.DeepEqual(backend.scans[1].Paths, []string{"app.py"}) {
		t.Fatalf("scan requests = %#v, want staged then strict selected path", backend.scans)
	}
	if backend.cacheClears != 1 || !reflect.DeepEqual(backend.installForces, []bool{true}) || len(backend.fixes) != 1 {
		t.Fatalf("operational backend calls: clears=%d installs=%#v fixes=%#v", backend.cacheClears, backend.installForces, backend.fixes)
	}
	for _, expected := range []string{"Welcome to PhantomGuard", "Pipeline: connected locally", "STAGED-CHANGES SCAN", "SELECTED-FILES SCAN", "CACHE", "Installed PhantomGuard hook", "Applied and re-validated", "PhantomGuard workspace closed"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestWelcomeStaysWithinNarrowTerminalAndHasPlainFallback(t *testing.T) {
	var output bytes.Buffer
	const width = 24
	RenderWelcome(&output, width, false, DefaultTheme, WelcomeModel{Status: Status{Repository: "repository-with-a-long-name", Branch: "feature/very-long-branch", FailMode: "warn"}, Version: "v0.1.0", Build: "local"})
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if got := runeWidth(line); got > width {
			t.Errorf("line width = %d, want <= %d: %q", got, width, line)
		}
		if strings.Contains(line, "\x1b[") {
			t.Errorf("plain fallback contains ANSI sequence: %q", line)
		}
	}
	if !strings.Contains(output.String(), "PhantomGuard") || !strings.Contains(output.String(), "Start: scan") {
		t.Fatalf("compact splash is missing essential content:\n%s", output.String())
	}
}

func TestParseArgsRejectsUnknownOptions(t *testing.T) {
	_, _, _, err := parseArgs([]string{"--bright-purple"})
	if err == nil {
		t.Fatal("parseArgs accepted unknown option")
	}
}

func TestScannerBackendUsesStagedContentAndSharedScanPipeline(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("import reqeusts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "app.py")

	registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/pypi/reqeusts/json" {
			t.Fatalf("registry path = %q", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer registry.Close()
	cache, err := validator.NewCache(filepath.Join(root, "cache", "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints.PyPI = registry.URL + "/pypi"
	client.Endpoints.NPM = registry.URL
	backend := &ScannerBackend{root: root, registry: client, cache: cache}

	result, err := backend.Scan(context.Background(), ScanRequest{Target: TargetStaged})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked || result.Report.FilesScanned != 1 || len(result.Report.Results) != 1 || result.Report.Results[0].Status != model.Phantom {
		t.Fatalf("unexpected TUI backend result: %#v", result)
	}
}

func TestScannerBackendOperationalMethodsUseProductionSafeguards(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	cache, err := validator.NewCache(filepath.Join(root, "cache", "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.PyPI, "requests", model.Exists); err != nil {
		t.Fatal(err)
	}
	backend := &ScannerBackend{root: root, registry: validator.NewClient(), cache: cache}

	before, err := backend.CacheStatus(context.Background())
	if err != nil || before.Entries != 1 || before.Verified != 1 {
		t.Fatalf("cache status before clear = %#v, %v", before, err)
	}
	if err := backend.ClearCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := backend.CacheStatus(context.Background())
	if err != nil || after.Entries != 0 {
		t.Fatalf("cache status after clear = %#v, %v", after, err)
	}
	hook, err := backend.Install(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hook); err != nil {
		t.Fatalf("installed hook unavailable at %s: %v", hook, err)
	}
}

func TestScannerBackendFixUsesVerifiedPreviewAndConfirmation(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	path := filepath.Join(root, "app.py")
	if err := os.WriteFile(path, []byte("import reqeusts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/pypi/requests/json" {
			t.Fatalf("registry path = %q", request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer registry.Close()
	cache, err := validator.NewCache(filepath.Join(root, "cache", "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := validator.NewClient()
	client.Endpoints.PyPI = registry.URL + "/pypi"
	client.Endpoints.NPM = registry.URL
	backend := &ScannerBackend{root: root, registry: client, cache: cache}
	var output bytes.Buffer
	err = backend.Fix(context.Background(), FixRequest{
		Path: "app.py", From: "reqeusts", To: "requests", Ecosystem: model.PyPI,
	}, strings.NewReader("y\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "import requests\n" {
		t.Fatalf("fixed contents = %q, %v", contents, err)
	}
	if !strings.Contains(output.String(), "Apply this verified replacement?") || !strings.Contains(output.String(), "Applied and re-validated") {
		t.Fatalf("fix output did not retain the preview confirmation flow:\n%s", output.String())
	}
}

func TestScannerBackendStrictStagedScanBlocksSkippedCandidate(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	contents := "import requests\n" + strings.Repeat("# padding\n", maxInputFileSize/8+1)
	if err := os.WriteFile(filepath.Join(root, "large.py"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "large.py")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	backend, err := NewScannerBackend(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Scan(context.Background(), ScanRequest{Target: TargetStaged, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked || len(result.Report.AnalysisIncomplete) == 0 {
		t.Fatalf("strict staged TUI scan allowed skipped candidate: %#v", result)
	}
}

func TestParseCommandsSupportsStrictPathsFixAndQuotes(t *testing.T) {
	scan, err := parseScanRequest([]string{"src/app.py", "src/test.py", "--strict"})
	if err != nil || scan.Target != TargetPaths || !scan.Strict || !reflect.DeepEqual(scan.Paths, []string{"src/app.py", "src/test.py"}) {
		t.Fatalf("scan request = %#v, %v", scan, err)
	}
	fix, err := parseFixRequest([]string{"--file", "folder with spaces/app.py", "--from", "reqeusts", "--to", "requests", "--ecosystem", "pypi"})
	if err != nil || fix.Path != "folder with spaces/app.py" || fix.Ecosystem != model.PyPI {
		t.Fatalf("fix request = %#v, %v", fix, err)
	}
	parts, err := splitCommand(`scan "folder with spaces/app.py" --strict`)
	if err != nil || !reflect.DeepEqual(parts, []string{"scan", "folder with spaces/app.py", "--strict"}) {
		t.Fatalf("command parts = %#v, %v", parts, err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(arguments, " "), output, err)
	}
}

func TestRenderScanResultNumbersRowsUnderColumnTitles(t *testing.T) {
	var out bytes.Buffer
	result := ScanResult{
		Report: model.Report{Results: []model.Result{
			{Finding: model.Finding{Path: "demo/a.py", Line: 5, Ecosystem: model.PyPI}, Package: "phantom-pkg", Status: model.Phantom},
			{Finding: model.Finding{Path: "demo/b.go", Line: 7, Ecosystem: model.Go}, Package: "example.com/not-real", Status: model.Unknown},
			{Finding: model.Finding{Path: "demo/c.py", Line: 1, Ecosystem: model.PyPI}, Package: "requests", Status: model.Exists},
		}},
		FailMode: "warn",
	}
	renderScanResult(&out, true, DefaultTheme, 120, TargetAll, result)
	lines := strings.Split(out.String(), "\n")
	headerIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "FILE:LINE") {
			headerIndex = index
		}
	}
	if headerIndex < 0 {
		t.Fatalf("column titles missing from output:\n%s", out.String())
	}
	if lines[headerIndex-1] != "" {
		t.Fatalf("expected a blank line between the summary and the column titles, got %q", lines[headerIndex-1])
	}
	if strings.Contains(lines[headerIndex], "\x1b[") || strings.Contains(lines[headerIndex+1], "\x1b[") {
		t.Fatalf("column titles and rule must be unstyled default text")
	}
	header := lines[headerIndex]
	for _, title := range []string{"#", "FILE:LINE", "STATUS", "PACKAGE", "REGISTRY"} {
		if !strings.Contains(header, title) {
			t.Fatalf("header %q lacks column title %q", header, title)
		}
	}
	if !strings.Contains(lines[headerIndex+1], "─") {
		t.Fatalf("expected rule under column titles, got %q", lines[headerIndex+1])
	}
	var rows []string
	for _, line := range lines[headerIndex+2:] {
		if strings.HasPrefix(line, fmt.Sprintf("  %d  ", len(rows)+1)) {
			rows = append(rows, line)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 numbered rows, got %d:\n%s", len(rows), out.String())
	}
	for column, value := range map[string]string{"FILE:LINE": "demo/a.py:5", "STATUS": "PHANTOM", "PACKAGE": "phantom-pkg", "REGISTRY": "pypi"} {
		if strings.Index(rows[0], value) != strings.Index(header, column) {
			t.Fatalf("value %q is not aligned under column %q:\n%s\n%s", value, column, header, rows[0])
		}
	}
}
