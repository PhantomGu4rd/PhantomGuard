//go:build windows

package ai

// Windows ACLs are not represented by os.FileMode. SaveLocalConfig still
// requests owner-only modes where supported; platform ACL validation belongs to
// the installer rather than a misleading Unix-mode check here.
func validateSecureLocalConfig(string) error { return nil }
