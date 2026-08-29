package gitio

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedHookPrefersOfficialInstallThenFallsBackToPATH(t *testing.T) {
	if hookMarker == "" {
		t.Fatal("hook marker must be present")
	}
	if strings.Contains(hookBody, "exec ./bin/phantomguard") || strings.Contains(hookBody, "exec \"$root/bin/phantomguard") {
		t.Fatalf("hook must not execute a repository-controlled binary:\n%s", hookBody)
	}
	if !strings.Contains(hookBody, "$LOCALAPPDATA/PhantomGuard/bin/phantomguard.exe") {
		t.Fatalf("hook must prefer the official Windows installation:\n%s", hookBody)
	}
	if !strings.Contains(hookBody, "command -v phantomguard") || !strings.Contains(hookBody, "exec phantomguard verify --strict") {
		t.Fatalf("hook must fall back to deterministic strict verification on PATH:\n%s", hookBody)
	}
	if strings.Contains(hookBody, " scan ") || strings.Contains(hookBody, "--ai") {
		t.Fatalf("hook must not invoke scan or AI:\n%s", hookBody)
	}
}

func TestInstallHookUsesGitPathForLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	runHookGit(t, root, "init", "-q")
	runHookGit(t, root, "config", "user.email", "phantomguard-test@example.test")
	runHookGit(t, root, "config", "user.name", "PhantomGuard Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runHookGit(t, root, "add", "README.md")
	runHookGit(t, root, "commit", "-qm", "seed")
	linked := filepath.Join(t.TempDir(), "linked-worktree")
	runHookGit(t, root, "worktree", "add", "--detach", linked, "HEAD")
	gitFile, err := os.Stat(filepath.Join(linked, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if gitFile.IsDir() {
		t.Fatal("fixture did not create a linked-worktree .git pointer file")
	}

	hook, err := InstallHook(linked, false)
	if err != nil {
		t.Fatalf("install hook in linked worktree: %v", err)
	}
	if _, err := os.Stat(hook); err != nil {
		t.Fatalf("installed hook is missing at %s: %v", hook, err)
	}
	if strings.Contains(filepath.ToSlash(hook), filepath.ToSlash(filepath.Join(linked, ".git", "hooks"))) {
		t.Fatalf("hook was incorrectly placed beneath linked worktree .git file: %s", hook)
	}
}

func runHookGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
