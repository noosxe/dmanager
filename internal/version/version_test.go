package version

import "testing"

// setBuildMeta overrides the linker-injected vars for the test and restores
// them afterwards.
func setBuildMeta(t *testing.T, version, commit, date string) {
	t.Helper()
	origVersion, origCommit, origDate := Version, Commit, Date
	Version, Commit, Date = version, commit, date
	t.Cleanup(func() { Version, Commit, Date = origVersion, origCommit, origDate })
}

func TestDisplayDevAppendsShortCommit(t *testing.T) {
	setBuildMeta(t, "dev", "0123456789abcdef0123456789abcdef01234567", "unknown")
	if got := Display(); got != "dev+0123456" {
		t.Errorf("Display() = %q, want %q", got, "dev+0123456")
	}
}

func TestDisplayReleasePassesThrough(t *testing.T) {
	setBuildMeta(t, "v0.8.0", "0123456789abcdef0123456789abcdef01234567", "unknown")
	if got := Display(); got != "v0.8.0" {
		t.Errorf("Display() = %q, want %q", got, "v0.8.0")
	}
}

func TestDevVersionDirtySuffix(t *testing.T) {
	setBuildMeta(t, "dev", "none", "unknown")
	if got := devVersion("0123456", true); got != "dev+0123456-dirty" {
		t.Errorf("devVersion(dirty) = %q, want %q", got, "dev+0123456-dirty")
	}
	if got := devVersion("0123456", false); got != "dev+0123456" {
		t.Errorf("devVersion(clean) = %q, want %q", got, "dev+0123456")
	}
}

func TestTruncateShortRevision(t *testing.T) {
	if got := truncate("0123456789abcdef"); got != "0123456" {
		t.Errorf("truncate(long) = %q, want %q", got, "0123456")
	}
	if got := truncate("abc"); got != "abc" {
		t.Errorf("truncate(short) = %q, want %q", got, "abc")
	}
}

func TestStringDevWithoutInjection(t *testing.T) {
	setBuildMeta(t, "dev", "none", "unknown")
	if got, want := String(), Display(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringReleaseIncludesCommitAndDate(t *testing.T) {
	setBuildMeta(t, "v0.8.0", "0123456789abcdef0123456789abcdef01234567", "2026-01-02T03:04:05Z")
	want := "v0.8.0 (commit 0123456789abcdef0123456789abcdef01234567, built 2026-01-02T03:04:05Z)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
