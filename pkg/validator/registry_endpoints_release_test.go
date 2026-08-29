//go:build !phantomguard_test_registry

package validator

import "testing"

func TestNewClientIgnoresRegistryOverrideEnvironmentInReleaseBuild(t *testing.T) {
	t.Setenv("PHANTOMGUARD_TEST_MODE", "1")
	t.Setenv("PHANTOMGUARD_TEST_REGISTRY_URL", "http://127.0.0.1:65535")

	client := NewClient()
	if client.Endpoints != publicRegistryEndpoints() {
		t.Fatalf("release client accepted environment registry override: %#v", client.Endpoints)
	}
}
