package gitio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const hookMarker = "Installed by PhantomGuard"
const hookChainMarker = "# PHANTOMGUARD_PREVIOUS_HOOK\n"

const hookBody = `#!/bin/sh
# Installed by PhantomGuard.
` + hookChainMarker + `# Prefer PhantomGuard's official per-user Windows installation. Git hooks may
# inherit an older GUI process PATH, where a stale Python console script can
# otherwise win command resolution.
if [ -n "${LOCALAPPDATA:-}" ] && [ -f "$LOCALAPPDATA/PhantomGuard/bin/phantomguard.exe" ]; then
  exec "$LOCALAPPDATA/PhantomGuard/bin/phantomguard.exe" verify --strict
fi

if ! command -v phantomguard >/dev/null 2>&1; then
  echo "PhantomGuard binary not found on PATH." >&2
  exit 1
fi
exec phantomguard verify --strict
`

// InstallHook installs the standard pre-commit hook and refuses to overwrite another hook without force.
func InstallHook(root string, force bool) (string, error) {
	hook, err := hookPath(root)
	if err != nil {
		return "", err
	}
	existing, err := os.ReadFile(hook)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing hook: %w", err)
	}
	contents := strings.Replace(hookBody, hookChainMarker, "", 1)
	if len(existing) > 0 && !strings.Contains(string(existing), hookMarker) {
		if !force {
			return "", fmt.Errorf("a foreign pre-commit hook exists; rerun install --force to back it up and chain it")
		}
		backup := hook + ".phantomguard-backup"
		if err := os.Rename(hook, backup); err != nil {
			return "", fmt.Errorf("back up foreign hook: %w", err)
		}
		contents = strings.Replace(hookBody, hookChainMarker, shellQuote(filepath.ToSlash(backup))+" || exit $?\n", 1)
	}
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		return "", fmt.Errorf("create hook directory: %w", err)
	}
	if err := os.WriteFile(hook, []byte(contents), 0o755); err != nil {
		return "", fmt.Errorf("write hook: %w", err)
	}
	return hook, nil
}

// hookPath asks Git for the hook location instead of assuming .git is a
// directory. Linked worktrees and submodules use a .git pointer file whose
// hooks live in a separate Git directory.
func hookPath(root string) (string, error) {
	output, err := command(root, "rev-parse", "--git-path", "hooks/pre-commit").Output()
	if err != nil {
		return "", fmt.Errorf("locate Git hooks: %w", err)
	}
	hook := strings.TrimSpace(string(output))
	if hook == "" {
		return "", fmt.Errorf("locate Git hooks: Git returned an empty hook path")
	}
	if !filepath.IsAbs(hook) {
		hook = filepath.Join(root, hook)
	}
	return filepath.Clean(hook), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
