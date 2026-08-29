// package_release builds self-contained archives for PhantomGuard releases.
// It uses only the Go standard library so release packaging works on Windows,
// Linux, and macOS without an additional package manager.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
)

type target struct {
	GOOS   string
	GOARCH string
}

var targets = []target{
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "windows", GOARCH: "amd64"},
	{GOOS: "windows", GOARCH: "arm64"},
}

func main() {
	version := flag.String("version", buildinfo.DefaultVersion, "release version used in artifact names and binary metadata")
	output := flag.String("out", "dist", "directory for release archives")
	flag.Parse()
	if err := validComponent(*version); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatal(fmt.Errorf("create output directory: %w", err))
	}

	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	var artifacts []string
	for _, item := range targets {
		artifact, err := buildArchive(root, *output, *version, item)
		if err != nil {
			fatal(err)
		}
		artifacts = append(artifacts, artifact)
		fmt.Println("Created", artifact)
	}
	if err := writeChecksums(*output, artifacts); err != nil {
		fatal(err)
	}
	fmt.Println("Created", filepath.Join(*output, "checksums.txt"))
}

func buildArchive(root, output, version string, item target) (string, error) {
	name := archiveBaseName(version, item)
	workspace, err := os.MkdirTemp("", "phantomguard-release-*")
	if err != nil {
		return "", fmt.Errorf("create package workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	packageDir := filepath.Join(workspace, name)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return "", fmt.Errorf("create package directory: %w", err)
	}
	binary := "phantomguard"
	if item.GOOS == "windows" {
		binary += ".exe"
	}
	if err := buildBinary(root, filepath.Join(packageDir, binary), item, version); err != nil {
		return "", err
	}
	installer := "install.sh"
	if item.GOOS == "windows" {
		installer = "install.ps1"
	}
	installerSource := filepath.Join(root, "installer", installer)
	installerDestination := filepath.Join(packageDir, installer)
	if item.GOOS != "windows" {
		err = copyUnixShellFile(installerSource, installerDestination, 0o755)
	} else {
		err = copyFile(installerSource, installerDestination, 0o755)
	}
	if err != nil {
		return "", err
	}
	for _, document := range []string{"README.md", "TUI_GUIDE.md", "LICENSE"} {
		if err := copyFile(filepath.Join(root, document), filepath.Join(packageDir, document), 0o644); err != nil {
			return "", err
		}
	}
	if err := addPlatformAssets(root, packageDir, binary, item, version); err != nil {
		return "", err
	}

	if item.GOOS == "windows" {
		archive := filepath.Join(output, name+".zip")
		return archive, createZip(archive, packageDir, name)
	}
	archive := filepath.Join(output, name+".tar.gz")
	return archive, createTarGz(archive, packageDir, name)
}

func archiveBaseName(version string, item target) string {
	return fmt.Sprintf("phantomguard_%s_%s_%s", version, item.GOOS, item.GOARCH)
}

func buildBinary(root, destination string, item target, version string) error {
	command := exec.Command("go", "build", "-trimpath", "-ldflags="+releaseLinkerFlags(version), "-o", destination, "./cmd/phantomguard")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+item.GOOS, "GOARCH="+item.GOARCH)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s/%s: %w\n%s", item.GOOS, item.GOARCH, err, output)
	}
	return nil
}

// addPlatformAssets adds the native desktop metadata needed for an icon to be
// shown by each operating system. Windows receives the icon in the executable
// resource section through checked-in .syso files. Linux and macOS have no
// portable icon field on a standalone command-line binary, so their archives
// carry the standard launcher or app-bundle metadata instead.
func addPlatformAssets(root, packageDir, binary string, item target, version string) error {
	switch item.GOOS {
	case "linux":
		return addLinuxDesktopAssets(root, packageDir)
	case "darwin":
		return addMacAppBundle(root, packageDir, binary, version)
	default:
		return nil
	}
}

func addLinuxDesktopAssets(root, packageDir string) error {
	iconDestination := filepath.Join(packageDir, "share", "icons", "hicolor", "768x768", "apps", "phantomguard.png")
	if err := copyFile(filepath.Join(root, "assets", "icons", "phantomguard.png"), iconDestination, 0o644); err != nil {
		return fmt.Errorf("stage Linux icon: %w", err)
	}
	desktopDestination := filepath.Join(packageDir, "share", "applications", "phantomguard.desktop")
	if err := writeTextFile(desktopDestination, linuxDesktopEntry, 0o644); err != nil {
		return fmt.Errorf("stage Linux desktop launcher: %w", err)
	}
	return nil
}

func addMacAppBundle(root, packageDir, binary, version string) error {
	contents := filepath.Join(packageDir, "PhantomGuard.app", "Contents")
	macOSDirectory := filepath.Join(contents, "MacOS")
	resourcesDirectory := filepath.Join(contents, "Resources")
	if err := os.MkdirAll(macOSDirectory, 0o755); err != nil {
		return fmt.Errorf("create macOS app executable directory: %w", err)
	}
	if err := os.MkdirAll(resourcesDirectory, 0o755); err != nil {
		return fmt.Errorf("create macOS app resources directory: %w", err)
	}
	if err := copyFile(filepath.Join(packageDir, binary), filepath.Join(macOSDirectory, binary), 0o755); err != nil {
		return fmt.Errorf("stage macOS app binary: %w", err)
	}
	if err := copyFile(filepath.Join(root, "assets", "icons", "PhantomGuardIcon.icns"), filepath.Join(resourcesDirectory, "PhantomGuardIcon.icns"), 0o644); err != nil {
		return fmt.Errorf("stage macOS icon: %w", err)
	}
	if err := writeTextFile(filepath.Join(contents, "Info.plist"), macAppInfoPlist(version), 0o644); err != nil {
		return fmt.Errorf("stage macOS app metadata: %w", err)
	}
	return nil
}

const linuxDesktopEntry = `[Desktop Entry]
Version=1.0
Type=Application
Name=PhantomGuard
Comment=Deterministic dependency verifier
Exec=phantomguard tui
Icon=phantomguard
Terminal=true
Categories=Development;Security;
Keywords=dependency;security;git;
`

func macAppInfoPlist(version string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>PhantomGuard</string>
  <key>CFBundleExecutable</key>
  <string>phantomguard</string>
  <key>CFBundleIconFile</key>
  <string>PhantomGuardIcon</string>
  <key>CFBundleIdentifier</key>
  <string>io.phantomguard.cli</string>
  <key>CFBundleName</key>
  <string>PhantomGuard</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>%s</string>
  <key>CFBundleVersion</key>
  <string>%s</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
`, strings.TrimPrefix(version, "v"), version)
}

func writeTextFile(destination, contents string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, []byte(contents), mode)
}

func releaseLinkerFlags(version string) string {
	return "-s -w -X " + buildinfo.LinkerVersionVariable + "=" + version
}

func createZip(destination, source, rootName string) error {
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(destination)
		}
	}()
	writer := zip.NewWriter(file)
	walkErr := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(rootName, relative))
		if entry.IsDir() {
			_, err := writer.Create(name + "/")
			return err
		}
		return addZipFile(writer, name, path)
	})
	writerErr := writer.Close()
	fileErr := file.Close()
	if walkErr != nil {
		return walkErr
	}
	if writerErr != nil {
		return fmt.Errorf("finalize %s: %w", destination, writerErr)
	}
	if fileErr != nil {
		return fmt.Errorf("close %s: %w", destination, fileErr)
	}
	completed = true
	return nil
}

func addZipFile(writer *zip.Writer, name, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name
	header.Method = zip.Deflate
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	return copyContents(destination, path)
}

func createTarGz(destination, source, rootName string) error {
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(destination)
		}
	}()
	gzipWriter := gzip.NewWriter(file)
	writer := tar.NewWriter(gzipWriter)
	walkErr := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name := rootName
		if relative != "." {
			name = filepath.ToSlash(filepath.Join(rootName, relative))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		header.Mode = releaseTarMode(relative, entry.IsDir())
		if entry.IsDir() {
			header.Name += "/"
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
	tarErr := writer.Close()
	gzipErr := gzipWriter.Close()
	fileErr := file.Close()
	if walkErr != nil {
		return walkErr
	}
	if tarErr != nil {
		return fmt.Errorf("finalize %s: %w", destination, tarErr)
	}
	if gzipErr != nil {
		return fmt.Errorf("compress %s: %w", destination, gzipErr)
	}
	if fileErr != nil {
		return fmt.Errorf("close %s: %w", destination, fileErr)
	}
	completed = true
	return nil
}

// releaseTarMode makes Unix archives portable when they are created on a
// Windows host, where source-file execute bits are not available. The archive
// contains exactly one executable binary and one executable shell installer.
func releaseTarMode(relative string, directory bool) int64 {
	if directory {
		return 0o755
	}
	normalized := filepath.ToSlash(relative)
	switch normalized {
	case "phantomguard", "install.sh":
		return 0o755
	default:
		if strings.HasSuffix(normalized, "/Contents/MacOS/phantomguard") {
			return 0o755
		}
		return 0o644
	}
}

func writeChecksums(output string, artifacts []string) error {
	sort.Strings(artifacts)
	file, err := os.Create(filepath.Join(output, "checksums.txt"))
	if err != nil {
		return fmt.Errorf("create checksums: %w", err)
	}
	defer file.Close()
	for _, artifact := range artifacts {
		hash, err := fileSHA256(artifact)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(file, "%s  %s\n", hash, filepath.Base(artifact)); err != nil {
			return err
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", destination, err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// copyUnixShellFile makes the release installer portable even when a Windows
// checkout supplied CRLF source bytes. Unix shells treat a trailing CR as part
// of a command or interpreter argument, so the archive must always contain LF.
func copyUnixShellFile(source, destination string, mode os.FileMode) error {
	contents, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	contents = bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
	contents = bytes.ReplaceAll(contents, []byte("\r"), []byte("\n"))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", destination, err)
	}
	if err := os.WriteFile(destination, contents, mode); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	return nil
}

func copyContents(destination io.Writer, source string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, file)
	return err
}

func repositoryRoot() (string, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate release script")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(source))), nil
}

func validComponent(value string) error {
	if value == "" {
		return fmt.Errorf("version cannot be empty")
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._-", character)) {
			return fmt.Errorf("version contains unsupported character %q", character)
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "package release:", err)
	os.Exit(1)
}
