package tailscale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dmanager/internal/config"
	"tailscale.com/ipn/ipnstate"
)

const needsMachineAuth = "NeedsMachineAuth"

var errTransient = errors.New("tsnet.Up: backend: no route to control")

// fakeClock records requested sleeps instead of waiting.
type fakeClock struct{ sleeps []time.Duration }

func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	return nil
}

func TestRetryLoopBackoffIsCappedExponential(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	err := retryLoop(context.Background(),
		func(context.Context) error {
			calls++
			if calls <= 8 {
				return errTransient
			}
			return nil
		},
		func(int, error, time.Duration) {}, clock.sleep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []time.Duration{2, 4, 8, 16, 32, 60, 60, 60}
	if len(clock.sleeps) != len(want) {
		t.Fatalf("sleeps = %v", clock.sleeps)
	}
	for i, w := range want {
		if clock.sleeps[i] != w*time.Second {
			t.Errorf("sleep %d = %v, want %v", i, clock.sleeps[i], w*time.Second)
		}
	}
}

func TestRetryLoopTimeoutRetriesImmediatelyWithoutBackoff(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	var retries int
	err := retryLoop(context.Background(),
		func(context.Context) error {
			calls++
			if calls <= 3 {
				return fmt.Errorf("tsnet.Up: %w", context.DeadlineExceeded)
			}
			return nil
		},
		func(int, error, time.Duration) { retries++ }, clock.sleep)
	if err != nil || retries != 3 || len(clock.sleeps) != 0 {
		t.Fatalf("err=%v retries=%d sleeps=%v", err, retries, clock.sleeps)
	}
}

func TestRetryLoopStopsOnTerminalError(t *testing.T) {
	clock := &fakeClock{}
	terminal := classifyUpError(errors.New("tsnet.Up: backend: invalid key: API key not valid"))
	calls := 0
	err := retryLoop(context.Background(),
		func(context.Context) error { calls++; return terminal },
		func(int, error, time.Duration) { t.Error("must not retry a terminal error") }, clock.sleep)
	if !IsTerminal(err) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRetryLoopStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := retryLoop(ctx,
		func(context.Context) error { cancel(); return errTransient },
		func(int, error, time.Duration) {}, (&fakeClock{}).sleep)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClassifyUpError(t *testing.T) {
	tests := []struct {
		msg      string
		terminal bool
	}{
		{"tsnet.Up: backend: invalid key: API key abc not valid", true},
		{"tsnet.Up: backend: Invalid key: key abc not valid", true},
		// transient messages that merely contain "valid", "expired", "key"
		{"tsnet.Up: backend: x509: certificate is not valid yet", false},
		{"tsnet.Up: backend: certificate has expired or is not yet valid", false},
		{"tsnet.Up: backend: node key expired; renewing", false},
		{"tsnet.Up: backend: could not validate the DERP map", false},
		{"tsnet.Up: backend: no route to control", false},
		{"tsnet.Up: context deadline exceeded", false},
		{"tsnet.Up: running, but no ip", false},
		// not a backend message: never terminal even if it mentions a key
		{"dial tcp: invalid key in proxy url", false},
	}
	for _, tt := range tests {
		got := IsTerminal(classifyUpError(errors.New(tt.msg)))
		if got != tt.terminal {
			t.Errorf("classify(%q) terminal = %v, want %v", tt.msg, got, tt.terminal)
		}
	}
}

func testConfigWithDir(dir string) config.TailscaleConfig {
	return config.TailscaleConfig{AuthKey: testAuthKey, Hostname: testHostname, StateDir: dir, Port: 80}
}

func TestUnusableStateDirIsTerminal(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	node := New(testConfigWithDir(blocker), testLogger())
	if err := node.Start(context.Background()); !IsTerminal(err) {
		t.Fatalf("want terminal error, got %v", err)
	}
}

func TestConnectKeepsNodeStartingWhileWaiting(t *testing.T) {
	node := New(testConfigWithDir(t.TempDir()), testLogger())
	node.started.Store(true)
	node.probeFn = func(context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{BackendState: needsMachineAuth}, nil
	}
	calls := 0
	err := node.connect(context.Background(),
		func(context.Context) error {
			calls++
			if calls < 3 {
				return errTransient
			}
			return nil
		}, (&fakeClock{}).sleep)
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.startErr != nil {
		t.Errorf("a transient failure must not leave the node failed: %v", node.startErr)
	}
	if node.waitDetail == "" {
		t.Error("waitDetail should record the last transient failure")
	}
}

func TestSnapshotWhileRetryingIsStartingNotFailed(t *testing.T) {
	node := New(testConfigWithDir(t.TempDir()), testLogger())
	node.started.Store(true)
	node.probeFn = func(context.Context) (*ipnstate.Status, error) {
		return &ipnstate.Status{BackendState: needsMachineAuth}, nil
	}
	node.noteWaiting(errTransient)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap := node.Snapshot(ctx)
	if snap.State != StateStarting || snap.Err == "" || snap.BackendState != needsMachineAuth {
		t.Errorf("state=%q err=%q", snap.State, snap.Err)
	}
}

func TestDescribeBackendState(t *testing.T) {
	if got := describeBackendState(needsMachineAuth); got == needsMachineAuth || got == "" {
		t.Errorf("no hint for NeedsMachineAuth: %q", got)
	}
}
