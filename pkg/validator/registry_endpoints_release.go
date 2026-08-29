//go:build !phantomguard_test_registry

package validator

// newClientEndpoints is deliberately free of environment-controlled endpoint
// selection in release builds. The enforcement client must always use the
// public registries selected by the binary itself.
func newClientEndpoints() Endpoints {
	return publicRegistryEndpoints()
}
