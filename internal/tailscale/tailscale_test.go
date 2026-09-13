package tailscale

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dmanager/internal/config"
)

const (
	testAuthKey  = "tskey-auth-test"
	testHostname = "dm-test"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewConfiguresServer(t *testing.T) {
	cfg := config.TailscaleConfig{
		AuthKey:  testAuthKey,
		Hostname: testHostname,
		StateDir: "/tmp/dm-ts-state",
		Port:     8080,
	}

	node := New(cfg, testLogger())

	if node.Server().AuthKey != testAuthKey {
		t.Errorf("expected auth key to be set explicitly on the tsnet server, got %q", node.Server().AuthKey)
	}
	if node.Server().Hostname != testHostname {
		t.Errorf("expected hostname %q, got %q", testHostname, node.Server().Hostname)
	}
	if node.Server().Dir != "/tmp/dm-ts-state" {
		t.Errorf("expected state dir /tmp/dm-ts-state, got %q", node.Server().Dir)
	}
	if node.port != 8080 {
		t.Errorf("expected tailnet port 8080, got %d", node.port)
	}
}

func TestStartRejectsUnusableStateDir(t *testing.T) {
	// A path occupied by a regular file can never be MkdirAll'ed, so Start
	// fails before touching the network.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	cfg := config.TailscaleConfig{
		AuthKey:  testAuthKey,
		Hostname: testHostname,
		StateDir: blocker,
		Port:     80,
	}

	node := New(cfg, testLogger())
	if err := node.Start(context.Background()); err == nil {
		t.Error("expected Start to fail when the state dir cannot be created")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	cfg := config.TailscaleConfig{
		AuthKey:  testAuthKey,
		Hostname: testHostname,
		StateDir: t.TempDir(),
		Port:     80,
	}

	node := New(cfg, testLogger())
	if err := node.Close(); err != nil {
		t.Errorf("first Close must succeed, got %v", err)
	}
	if err := node.Close(); err != nil {
		t.Errorf("second Close must not report an error, got %v", err)
	}
}

func TestForwardedProtoHTTPSForcesHeader(t *testing.T) {
	var seen string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusOK)
	})

	// A spoofed inbound value must not survive: on the TLS listener https
	// is the truth, so Set overwrites whatever the client sent.
	h := forwardedProtoHTTPS(inner)
	req := httptest.NewRequest(http.MethodGet, "http://dmanager.example/v1/whoami", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if seen != "https" {
		t.Errorf("expected X-Forwarded-Proto forced to https, got %q", seen)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected inner handler to run, got status %d", w.Code)
	}
}

func TestSnapshotNilNode(t *testing.T) {
	// The disabled-feature case: serve.go passes the nil *Node straight
	// through the admin status source interface.
	var node *Node
	snap := node.Snapshot(context.Background())
	if snap.Enabled {
		t.Error("expected nil node to report disabled")
	}
	if snap.State != StateDisabled {
		t.Errorf("expected state %q, got %q", StateDisabled, snap.State)
	}
}

func TestSnapshotUnstartedNode(t *testing.T) {
	node := New(config.TailscaleConfig{
		AuthKey:      testAuthKey,
		Hostname:     testHostname,
		Port:         8080,
		HTTPSEnabled: true,
	}, testLogger())

	snap := node.Snapshot(context.Background())
	if !snap.Enabled {
		t.Error("expected constructed node to report enabled")
	}
	if snap.State != StateStarting {
		t.Errorf("expected state %q before Start returns, got %q", StateStarting, snap.State)
	}
	if snap.Hostname != testHostname {
		t.Errorf("expected hostname %q, got %q", testHostname, snap.Hostname)
	}
	if snap.Port != 8080 {
		t.Errorf("expected port 8080, got %d", snap.Port)
	}
	if !snap.HTTPSEnabled {
		t.Error("expected https_enabled true")
	}
}

func TestSnapshotFailedStart(t *testing.T) {
	node := New(config.TailscaleConfig{
		AuthKey:      testAuthKey,
		Hostname:     testHostname,
		StateDir:     filepath.Join(t.TempDir(), "state"),
		HTTPSEnabled: false,
	}, testLogger())

	// A pre-cancelled context makes Up fail immediately (degraded mode)
	// without contacting any control plane.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := node.Start(ctx); err == nil {
		t.Fatal("expected Start with cancelled context to fail")
	}

	snap := node.Snapshot(context.Background())
	if snap.State != StateFailed {
		t.Errorf("expected state %q, got %q", StateFailed, snap.State)
	}
	if snap.Err == "" {
		t.Error("expected failure detail in Err")
	}
	if snap.Enabled != true {
		t.Error("expected enabled true (auth key configured)")
	}
}
