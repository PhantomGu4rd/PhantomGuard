package buildinfo

import "testing"

func TestDefaultVersion(t *testing.T) {
	if Version != DefaultVersion {
		t.Fatalf("default Version = %q, want %q", Version, DefaultVersion)
	}
	if LinkerVersionVariable == "" {
		t.Fatal("LinkerVersionVariable must not be empty")
	}
}
