// Package buildinfo provides release metadata shared by every PhantomGuard
// entry point. Build scripts may replace Version with go build -ldflags -X.
package buildinfo

const (
	// DefaultVersion is the source-tree version used by local development
	// builds. Release builds replace Version with the tag being packaged.
	DefaultVersion = "v0.1.3"

	// LinkerVersionVariable is the fully-qualified Go linker variable name
	// used to inject a release tag into Version.
	LinkerVersionVariable = "github.com/phantomguard/phantomguard/pkg/buildinfo.Version"
)

// Version must remain a package-level string variable with a constant
// initializer so `go build -ldflags="-X ..."` can replace it.
var Version = DefaultVersion
