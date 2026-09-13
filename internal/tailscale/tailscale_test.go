package tailscale

import (
	"context"
	"io"
	"log/slog"
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
