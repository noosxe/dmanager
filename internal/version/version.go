// Package version exposes build metadata injected at link time via
// -ldflags "-X dmanager/internal/version.Version=<ver> -X dmanager/internal/version.Commit=<sha> -X dmanager/internal/version.Date=<isotime>".
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version is the release identifier, e.g. "v0.8.0" or "dev".
	Version = "dev"
	// Commit is the source revision the binary was built from.
	Commit = "none"
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// devVersion renders "dev+<sha>" for dev builds, suffixing "-dirty" when the
// source tree had uncommitted changes at build time. sha must be a non-empty
// short revision.
func devVersion(sha string, modified bool) string {
	if modified {
		return Version + "+" + sha + "-dirty"
	}
	return Version + "+" + sha
}

// shortCommit returns the short source revision of the running binary: the
// linker-injected Commit when present, otherwise the VCS stamp the Go
// toolchain embeds for plain builds from a git checkout (buildvcs). modified
// reports an unclean working tree when the stamp is the source.
func shortCommit() (sha string, modified bool) {
	if Commit != "" && Commit != "none" {
		return truncate(Commit), false
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			sha = truncate(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return sha, modified
}

// truncate shortens a revision to git's conventional 7-character short form.
func truncate(revision string) string {
	if len(revision) > 7 {
		return revision[:7]
	}
	return revision
}

// Display renders the concise version identifier surfaced in the UI and
// --version. Dev builds append the short commit sha ("dev+1a2b3c4") so a
// running instance can be tied back to the exact source revision; release
// versions pass through unchanged.
func Display() string {
	if Version != "dev" {
		return Version
	}
	sha, modified := shortCommit()
	if sha == "" {
		return Version
	}
	return devVersion(sha, modified)
}

// String renders the value reported by --version. Without linker injection
// (plain "go build") it degrades to the bare display version.
func String() string {
	if Commit == "none" {
		return Display()
	}
	return fmt.Sprintf("%s (commit %s, built %s)", Display(), Commit, Date)
}
