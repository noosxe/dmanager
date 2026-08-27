// Package version exposes build metadata injected at link time via
// -ldflags "-X dmanager/internal/version.Version=<ver> -X dmanager/internal/version.Commit=<sha> -X dmanager/internal/version.Date=<isotime>".
package version

import "fmt"

var (
	// Version is the release identifier, e.g. "v0.8.0" or "dev".
	Version = "dev"
	// Commit is the source revision the binary was built from.
	Commit = "none"
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// String renders the value reported by --version and the UI. Without linker
// injection (plain "go build") it degrades to the bare version string.
func String() string {
	if Commit == "none" {
		return Version
	}
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
