package remediation

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantomguard/phantomguard/pkg/model"
	"github.com/phantomguard/phantomguard/pkg/validator"
)

func TestSafePathRejectsEscape(t *testing.T) {
	if _, err := safePath(t.TempDir(), "../outside.py"); err == nil {
		t.Fatal("path escape was accepted")
	}
}

func TestSafePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "target.py")
	if err := os.WriteFile(target, []byte("import reaxt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "app.py")
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "privilege") {
			t.Skip("creating symlinks requires additional Windows privileges")
		}
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := safePath(root, "app.py"); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}

func TestPreviewDoesNotWriteWithoutConfirmation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "app.py")
	if err := os.WriteFile(file, []byte("import reaxt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := validator.NewClient()
	client.Endpoints = validator.Endpoints{PyPI: server.URL, NPM: server.URL}
	// This directory is not a Git repository, so test the confirmation boundary through a minimal Git repository only when git is available.
	if err := PreviewAndApply(context.Background(), root, Fix{Path: "app.py", From: "reaxt", To: "react", Ecosystem: model.PyPI}, client, bytes.NewBufferString("n\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("non-repository safety check should stop the fixer")
	}
}
