package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInstalledHookBlocksPhantomThenAllowsVerifiedFix exercises the compiled
// binary exactly as Git invokes it. Its registry is local and deterministic.
func TestInstalledHookBlocksPhantomThenAllowsVerifiedFix(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/pypi/requests/json":
			writer.WriteHeader(http.StatusOK)
		case "/pypi/reqeusts/json":
			writer.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected registry request: %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer registry.Close()
	t.Setenv("PHANTOMGUARD_TEST_MODE", "1")
	t.Setenv("PHANTOMGUARD_TEST_REGISTRY_URL", registry.URL)
	// The test-only binary writes registry results only to this isolated cache;
	// fake httptest statuses must never contaminate a developer's real cache.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// verify is the enforcement plane: an old manual-scan bypass must not
	// suppress the installed strict hook.
	t.Setenv("PHANTOMGUARD_SKIP", "1")
	aiConfigHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(aiConfigHome, "phantomguard"), 0o700); err != nil {
		t.Fatalf("create AI config fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiConfigHome, "phantomguard", "ai.json"), []byte("not valid JSON"), 0o600); err != nil {
		t.Fatalf("write AI config fixture: %v", err)
	}
	// A malformed optional AI config must not affect deterministic verification.
	t.Setenv("XDG_CONFIG_HOME", aiConfigHome)

	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "phantomguard-test@example.test")
	runGit(t, root, "config", "user.name", "PhantomGuard Test")

	binary := buildIntegrationBinary(t)
	prependPath(t, filepath.Dir(binary))
	runProgram(t, root, binary, "install")
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-commit")); err != nil {
		t.Fatalf("installed hook missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("import requests\nimport reqeusts\n"), 0o600); err != nil {
		t.Fatalf("write staged fixture: %v", err)
	}
	// This untracked module would make a working-tree local-package check think
	// the phantom import is valid. Verification must inspect only staged content.
	if err := os.WriteFile(filepath.Join(root, "reqeusts.py"), []byte("# intentionally untracked\n"), 0o600); err != nil {
		t.Fatalf("write untracked bypass fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("requests==2.32.0 --hash=sha256:0123456789abcdef\n"), 0o600); err != nil {
		t.Fatalf("write staged provenance fixture: %v", err)
	}
	runGit(t, root, "add", "app.py", "requirements.txt")
	// This unstaged policy would exempt the phantom if verification read the
	// working-tree config instead of the exact Git-index configuration.
	if err := os.WriteFile(filepath.Join(root, ".phantomguard.json"), []byte(`{"allow":["reqeusts"],"languages":[]}`), 0o600); err != nil {
		t.Fatalf("write unstaged policy bypass fixture: %v", err)
	}

	output, err := runGitResult(root, "commit", "-m", "introduce phantom dependency")
	if err == nil {
		t.Fatal("commit with a confirmed phantom dependency succeeded")
	}
	if !strings.Contains(output, "reqeusts") || !strings.Contains(output, "PHANTOM") {
		t.Fatalf("blocked commit did not report the phantom dependency:\n%s", output)
	}

	fix := exec.Command(binary, "fix", "--file", "app.py", "--from", "reqeusts", "--to", "requests", "--ecosystem", "pypi")
	fix.Dir = root
	fix.Env = os.Environ()
	fix.Stdin = strings.NewReader("y\n")
	if output, err := fix.CombinedOutput(); err != nil {
		t.Fatalf("verified fixer failed: %v\n%s", err, output)
	}
	runGit(t, root, "add", "app.py")
	runGit(t, root, "commit", "-m", "use verified dependency")
	if output := runGit(t, root, "show", "--format=%s", "--no-patch", "HEAD"); !strings.Contains(output, "use verified dependency") {
		t.Fatalf("fixed commit was not created:\n%s", output)
	}
}

func buildIntegrationBinary(t *testing.T) string {
	t.Helper()
	moduleRoot := findModuleRoot(t)
	name := "phantomguard"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	command := exec.Command("go", "build", "-tags", "phantomguard_test_registry", "-o", binary, "./cmd/phantomguard")
	command.Dir = moduleRoot
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-build-cache"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build integration binary: %v\n%s", err, output)
	}
	return binary
}

func prependPath(t *testing.T, directory string) {
	t.Helper()
	path := os.Getenv("PATH")
	if path == "" {
		t.Setenv("PATH", directory)
		return
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+path)
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	path, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			t.Fatal("could not find module root")
		}
		path = parent
	}
}

func runProgram(t *testing.T, directory, program string, arguments ...string) string {
	t.Helper()
	command := exec.Command(program, arguments...)
	command.Dir = directory
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", program, strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	output, err := runGitResult(directory, arguments...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return output
}

func runGitResult(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err
}
