package tailscale

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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

// selfSignedCert returns a throwaway localhost certificate so the h2 wiring
// tests below can run a real TLS listener without a tsnet backend.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// startH2CapableServer wires a TLS listener exactly the way ServeHTTPS does:
// the newTailnetTLSConfig shape (NextProtos h2+http/1.1, GetCertificate from
// a LocalClient) plus the http.Server Protocols enabling HTTP/2. The
// certificate is swapped for a self-signed one since no tsnet backend is
// present; everything else about the negotiation path is real.
func startH2CapableServer(t *testing.T, h http.Handler) string {
	t.Helper()
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, errors.New("no tsnet backend in test")
	}
	cfg := newTailnetTLSConfig(getCert)
	cfg.Certificates = []tls.Certificate{selfSignedCert(t)}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	srv.Protocols = new(http.Protocols)
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetHTTP2(true)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func TestTailnetTLSConfigAdvertisesH2(t *testing.T) {
	cfg := newTailnetTLSConfig(func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, errors.New("not used")
	})
	if got, want := cfg.NextProtos, []string{"h2", "http/1.1"}; !slices.Equal(got, want) {
		t.Errorf("NextProtos = %v, want %v", got, want)
	}
	if cfg.GetCertificate == nil {
		t.Error("GetCertificate must be set (delegates to the node's local client)")
	}
}

func TestH2WiringNegotiatesHTTP2(t *testing.T) {
	addr := startH2CapableServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// An HTTP/2-capable client must negotiate h2 via ALPN and get a real
	// HTTP/2 response through the registered handler.
	// HTTP/2-only client: offers just h2 via ALPN, so no silent HTTP/1.1
	// fallback is possible.
	h2Protocols := new(http.Protocols)
	h2Protocols.SetHTTP2(true)
	h2tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only self-signed listener
		Protocols:       h2Protocols,
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := h2tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("h2 round trip: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got, want := resp.Proto, "HTTP/2.0"; got != want {
		t.Errorf("h2-capable client negotiated %q, want %q", got, want)
	}

	// An HTTP/1.1-only client must keep working on the same listener.
	h1tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // test-only self-signed listener
	req1, err := http.NewRequest(http.MethodGet, "https://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp1, err := h1tr.RoundTrip(req1)
	if err != nil {
		t.Fatalf("h1 round trip: %v", err)
	}
	defer func() { _ = resp1.Body.Close() }()
	if got, want := resp1.Proto, "HTTP/1.1"; got != want {
		t.Errorf("h1 client negotiated %q, want %q", got, want)
	}
}
