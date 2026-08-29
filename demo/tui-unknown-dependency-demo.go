//go:build ignore

// This intentional TUI fixture is parsed by PhantomGuard but excluded from Go
// builds because its import is deliberately unknown.
package probe

import _ "example.com/not-a-real-module"
