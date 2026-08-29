//go:build !windows

package ai

import (
	"fmt"
	"os"
	"path/filepath"
)

// validateSecureLocalConfig rejects manually created or weakened advisory
// credentials before they can be used. SaveLocalConfig creates these modes;
// checking on load keeps the 0600/0700 promise true for existing files too.
func validateSecureLocalConfig(path string) error {
	file, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect AI configuration permissions: %w", err)
	}
	if !file.Mode().IsRegular() || file.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("AI configuration must be a regular owner-only file")
	}
	if file.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("AI configuration must have 0600 permissions; run phantomguard ai setup to rewrite it")
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("inspect AI configuration directory permissions: %w", err)
	}
	if !directory.IsDir() || directory.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("AI configuration directory must have 0700 permissions; run phantomguard ai setup to rewrite it")
	}
	return nil
}
