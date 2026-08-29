package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"debug/pe"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
)

func TestArchiveBaseName(t *testing.T) {
	got := archiveBaseName("v0.1.3", target{GOOS: "windows", GOARCH: "arm64"})
	if want := "phantomguard_v0.1.3_windows_arm64"; got != want {
		t.Fatalf("archiveBaseName() = %q, want %q", got, want)
	}
}

func TestReleaseLinkerFlagsEmbedVersion(t *testing.T) {
	got := releaseLinkerFlags("v0.1.3")
	want := "-s -w -X " + buildinfo.LinkerVersionVariable + "=v0.1.3"
	if got != want {
		t.Fatalf("releaseLinkerFlags() = %q, want %q", got, want)
	}
}

func TestReleaseTargetsCoverSupportedPlatforms(t *testing.T) {
	seen := make(map[target]bool, len(targets))
	for _, item := range targets {
		seen[item] = true
	}
	for _, item := range []target{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "arm64"},
	} {
		if !seen[item] {
			t.Errorf("release target missing: %s/%s", item.GOOS, item.GOARCH)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("release target count = %d, want 6", len(seen))
	}
}

func TestCreateTarGzSetsPortableExecutableModes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"phantomguard", "install.sh", "README.md", "TUI_GUIDE.md", "LICENSE"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	macBinary := filepath.Join(source, "PhantomGuard.app", "Contents", "MacOS", "phantomguard")
	if err := os.MkdirAll(filepath.Dir(macBinary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(macBinary, []byte("macOS binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "release.tar.gz")
	if err := createTarGz(archive, source, "phantomguard_v0.1.3_linux_amd64"); err != nil {
		t.Fatalf("createTarGz: %v", err)
	}

	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	modes := make(map[string]int64)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		modes[header.Name] = header.Mode
	}

	root := "phantomguard_v0.1.3_linux_amd64"
	for name, want := range map[string]int64{
		root + "/":             0o755,
		root + "/phantomguard": 0o755,
		root + "/install.sh":   0o755,
		root + "/README.md":    0o644,
		root + "/TUI_GUIDE.md": 0o644,
		root + "/LICENSE":      0o644,
		root + "/PhantomGuard.app/Contents/MacOS/phantomguard": 0o755,
	} {
		if got, ok := modes[name]; !ok {
			t.Errorf("archive is missing %s", name)
		} else if got != want {
			t.Errorf("mode for %s = %04o, want %04o", name, got, want)
		}
	}
}

func TestCopyUnixShellFileNormalizesWindowsLineEndings(t *testing.T) {
	source := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(source, []byte("#!/usr/bin/env sh\r\nset -eu\r\necho ready\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "package", "install.sh")
	if err := copyUnixShellFile(source, destination, 0o755); err != nil {
		t.Fatalf("copy Unix shell installer: %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte("\r")) {
		t.Fatalf("Unix shell installer contains CR bytes: %q", contents)
	}
	if got, want := string(contents), "#!/usr/bin/env sh\nset -eu\necho ready\n"; got != want {
		t.Fatalf("normalized installer = %q, want %q", got, want)
	}
}

func TestAddPlatformAssetsAddsNativeIconMetadata(t *testing.T) {
	root := t.TempDir()
	icons := filepath.Join(root, "assets", "icons")
	if err := os.MkdirAll(icons, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(icons, "phantomguard.png"), []byte("linux icon"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(icons, "PhantomGuardIcon.icns"), []byte("macOS icon"), 0o600); err != nil {
		t.Fatal(err)
	}

	linuxPackage := filepath.Join(t.TempDir(), "linux")
	if err := os.MkdirAll(linuxPackage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := addPlatformAssets(root, linuxPackage, "phantomguard", target{GOOS: "linux", GOARCH: "amd64"}, "v0.1.3"); err != nil {
		t.Fatalf("add Linux platform assets: %v", err)
	}
	linuxIcon, err := os.ReadFile(filepath.Join(linuxPackage, "share", "icons", "hicolor", "768x768", "apps", "phantomguard.png"))
	if err != nil || string(linuxIcon) != "linux icon" {
		t.Fatalf("Linux icon = %q, read error = %v", linuxIcon, err)
	}
	launcher, err := os.ReadFile(filepath.Join(linuxPackage, "share", "applications", "phantomguard.desktop"))
	if err != nil || !strings.Contains(string(launcher), "Exec=phantomguard tui") || !strings.Contains(string(launcher), "Icon=phantomguard") {
		t.Fatalf("Linux desktop launcher = %q, read error = %v", launcher, err)
	}

	macPackage := filepath.Join(t.TempDir(), "macOS")
	if err := os.MkdirAll(macPackage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macPackage, "phantomguard"), []byte("macOS binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := addPlatformAssets(root, macPackage, "phantomguard", target{GOOS: "darwin", GOARCH: "arm64"}, "v0.1.3"); err != nil {
		t.Fatalf("add macOS platform assets: %v", err)
	}
	macIcon, err := os.ReadFile(filepath.Join(macPackage, "PhantomGuard.app", "Contents", "Resources", "PhantomGuardIcon.icns"))
	if err != nil || string(macIcon) != "macOS icon" {
		t.Fatalf("macOS icon = %q, read error = %v", macIcon, err)
	}
	info, err := os.ReadFile(filepath.Join(macPackage, "PhantomGuard.app", "Contents", "Info.plist"))
	if err != nil || !strings.Contains(string(info), "CFBundleIconFile") || !strings.Contains(string(info), "PhantomGuardIcon") {
		t.Fatalf("macOS Info.plist = %q, read error = %v", info, err)
	}
}

func TestWindowsReleaseBinaryEmbedsResourceSection(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		binary := filepath.Join(t.TempDir(), "phantomguard_"+arch+".exe")
		if err := buildBinary(root, binary, target{GOOS: "windows", GOARCH: arch}, "v0.1.3"); err != nil {
			t.Fatalf("build Windows/%s release binary: %v", arch, err)
		}
		image, err := pe.Open(binary)
		if err != nil {
			t.Fatalf("open Windows/%s binary: %v", arch, err)
		}
		hasResources := false
		for _, section := range image.Sections {
			if section.Name == ".rsrc" && section.Size > 0 {
				hasResources = true
				break
			}
		}
		if err := image.Close(); err != nil {
			t.Fatalf("close Windows/%s binary: %v", arch, err)
		}
		if !hasResources {
			t.Fatalf("Windows/%s release binary is missing a non-empty .rsrc icon section", arch)
		}
	}
}

func TestCreateZipContainsReadablePackageFiles(t *testing.T) {
	source := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"phantomguard.exe", "install.ps1", "README.md", "TUI_GUIDE.md", "LICENSE"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "release.zip")
	root := "phantomguard_v0.1.3_windows_amd64"
	if err := createZip(archive, source, root); err != nil {
		t.Fatalf("createZip: %v", err)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("open finalized ZIP: %v", err)
	}
	defer reader.Close()
	contents := make(map[string]string, len(reader.File))
	for _, entry := range reader.File {
		input, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, readErr := io.ReadAll(input)
		closeErr := input.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		contents[entry.Name] = string(raw)
	}
	for _, name := range []string{"phantomguard.exe", "install.ps1", "README.md", "TUI_GUIDE.md", "LICENSE"} {
		path := root + "/" + name
		if got, ok := contents[path]; !ok || got != name {
			t.Errorf("ZIP entry %s = %q, present=%t", path, got, ok)
		}
	}
}
