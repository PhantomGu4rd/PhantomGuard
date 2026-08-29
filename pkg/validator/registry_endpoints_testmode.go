//go:build phantomguard_test_registry

package validator

import (
	"os"
	"strings"
)

// newClientEndpoints permits the compiled hook integration test to use its
// local httptest registry. This file is excluded from every release build; a
// user environment can never redirect a shipped PhantomGuard binary.
func newClientEndpoints() Endpoints {
	endpoints := publicRegistryEndpoints()
	if os.Getenv("PHANTOMGUARD_TEST_MODE") != "1" {
		return endpoints
	}
	base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("PHANTOMGUARD_TEST_REGISTRY_URL")), "/")
	if base == "" {
		return endpoints
	}
	return Endpoints{PyPI: base + "/pypi", NPM: base}
}
