//go:build phantomguard_test_registry

package validator

import "testing"

func TestTaggedIntegrationClientUsesExplicitTestRegistry(t *testing.T) {
	t.Setenv("PHANTOMGUARD_TEST_MODE", "1")
	t.Setenv("PHANTOMGUARD_TEST_REGISTRY_URL", "http://127.0.0.1:32123/")

	client := NewClient()
	if client.Endpoints != (Endpoints{PyPI: "http://127.0.0.1:32123/pypi", NPM: "http://127.0.0.1:32123"}) {
		t.Fatalf("tagged client did not use explicit test registry: %#v", client.Endpoints)
	}
}
