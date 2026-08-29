package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedContentsOverridesAutomaticIgnorePolicy(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "demo", "fixture.py")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("import phantom_fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, notices, _, err := selectedContents(root, []string{"demo/fixture.py"})
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

func TestWorkingContentsReportsIgnoredFixtureDirectory(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "demo", "fixture.py")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("import phantom_fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, notices, _, err := workingContents(root, []string{"demo/**"})
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
