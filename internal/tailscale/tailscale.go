// Package tailscale embeds a Tailscale node (tsnet) into the dmanager
// process so the web application is reachable directly from a tailnet
// without published ports or a sidecar container (see docs/tailscale.md).
package tailscale

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dmanager/internal/config"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// Node owns the embedded tsnet lifecycle for one dmanager process.
type Node struct {
	srv    *tsnet.Server
	port   int
	logger *slog.Logger

	hostname     string
	httpsEnabled bool

	closing   atomic.Bool
	started   atomic.Bool
	closeOnce sync.Once
	closeErr  error

	// mu guards the status fields below, written by the Start goroutine
	// and the Snapshot probe, read by Snapshot from RPC handlers.
	mu           sync.Mutex
	startErr     error
	backendState string
	dnsName      string
	ips          []string
	certDomains  []string
	keyExpiry    time.Time // zero = no expiry known
}

// New constructs an unstarted node from the resolved configuration. All
// tsnet.Server fields (including AuthKey) are set explicitly so tsnet never
// falls back to reading the TS_AUTHKEY environment variable implicitly.
func New(cfg config.TailscaleConfig, logger *slog.Logger) *Node {
	srv := &tsnet.Server{
		AuthKey:  cfg.AuthKey,
		Hostname: cfg.Hostname,
		Dir:      cfg.StateDir,
	}
	// UserLogf carries operator-relevant messages (login URLs, state
	// changes); Logf is the verbose backend debug stream. Neither ever
	// receives the auth key itself.
	srv.UserLogf = func(format string, args ...any) {
		logger.Info(fmt.Sprintf(format, args...))
	}
	srv.Logf = func(format string, args ...any) {
		logger.Debug(fmt.Sprintf(format, args...))
	}
	return &Node{
		srv:          srv,
		port:         cfg.Port,
		logger:       logger,
		hostname:     cfg.Hostname,
		httpsEnabled: cfg.HTTPSEnabled,
	}
}

// Server exposes the underlying tsnet server for inspection in tests.
func (n *Node) Server() *tsnet.Server { return n.srv }

// Start connects the node to the tailnet, blocking until it is running or
// ctx is done. The state directory is created with owner-only permissions
// if it does not exist yet (it holds the node's private keys).
func (n *Node) Start(ctx context.Context) error {
	// tsnet requires the state directory to exist before startup.
	if err := os.MkdirAll(n.srv.Dir, 0o700); err != nil {
		return fmt.Errorf("failed to create tailscale state directory %s: %w", n.srv.Dir, err)
	}

	// Mark the node as started before Up so Close knows tsnet internals
	// may have been initialized and require an explicit shutdown.
	n.started.Store(true)

	status, err := n.srv.Up(ctx)
	if err != nil {
		err = fmt.Errorf("tailscale node failed to connect: %w", err)
		n.mu.Lock()
		n.startErr = err
		n.mu.Unlock()
		return err
	}
	n.cacheStatus(status)

	ips := statusIPs(status)
	attrs := []any{"ips", ips, "state", status.BackendState}
	if status.Self != nil {
		if dnsName := strings.TrimSuffix(status.Self.DNSName, "."); dnsName != "" {
			attrs = append(attrs, "dns_name", dnsName)
		}
	}
	if len(status.CertDomains) > 0 {
		attrs = append(attrs, "cert_domains", status.CertDomains)
	}
	n.logger.Info("tailscale node online", attrs...)
	return nil
}

// Node lifecycle states reported by Snapshot.
const (
	// StateDisabled is reported by a nil Node — feature inert (no auth key).
	StateDisabled = "disabled"
	// StateStarting covers a constructed node whose Start has not returned yet.
	StateStarting = "starting"
	// StateRunning marks a node that connected; BackendState carries live detail.
	StateRunning = "running"
	// StateFailed marks a node whose Start returned an error (degraded mode).
	StateFailed = "failed"
)

// Snapshot is a point-in-time status of the embedded node, shaped for the
// AdminService status RPC (docs/tailscale.md §9 Q8/Q11). It never contains
// key material — only identity and health data already exposed in logs.
type Snapshot struct {
	Enabled      bool
	State        string // one of the State* constants
	BackendState string // live ipn state, e.g. "Running"; empty when unprobed
	Hostname     string
	DNSName      string // full MagicDNS name, no trailing dot; empty pre-connect
	IPs          []string
	Port         int
	HTTPSEnabled bool
	CertDomains  []string
	KeyExpiry    time.Time // zero = no expiry known
	Err          string    // failure detail; empty when healthy
}

// Snapshot returns the node's current status. It is safe to call on a nil
// Node (reports the disabled state), concurrently from RPC handlers, and at
// any lifecycle stage. A live probe via the node's local API refreshes the
// cached identity fields when reachable; on probe failure the last known
// values are returned with Err set. Callers bound the probe via ctx.
func (n *Node) Snapshot(ctx context.Context) Snapshot {
	if n == nil {
		return Snapshot{Enabled: false, State: StateDisabled}
	}
	snap := Snapshot{
		Enabled:      true,
		Hostname:     n.hostname,
		Port:         n.port,
		HTTPSEnabled: n.httpsEnabled,
	}
	if !n.started.Load() {
		snap.State = StateStarting
		return snap
	}

	n.mu.Lock()
	if n.startErr != nil {
		err := n.startErr.Error()
		n.mu.Unlock()
		snap.State = StateFailed
		snap.Err = err
		return snap
	}
	n.mu.Unlock()
	snap.State = StateRunning

	// Best-effort live refresh; falls back to the values cached at Start.
	st, probeErr := n.probeStatus(ctx)
	if probeErr == nil {
		n.cacheStatus(st)
	} else {
		snap.Err = "live status unavailable: " + probeErr.Error()
	}

	n.mu.Lock()
	snap.BackendState = n.backendState
	snap.DNSName = n.dnsName
	snap.IPs = slices.Clone(n.ips)
	snap.CertDomains = slices.Clone(n.certDomains)
	snap.KeyExpiry = n.keyExpiry
	n.mu.Unlock()
	return snap
}

func statusIPs(st *ipnstate.Status) []string {
	ips := make([]string, 0, len(st.TailscaleIPs))
	for _, ip := range st.TailscaleIPs {
		ips = append(ips, ip.String())
	}
	return ips
}

// cacheStatus refreshes the identity fields cached for Snapshot from a
// status result obtained via Up or the live local-API probe.
func (n *Node) cacheStatus(st *ipnstate.Status) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.backendState = st.BackendState
	n.ips = statusIPs(st)
	if st.Self != nil {
		n.dnsName = strings.TrimSuffix(st.Self.DNSName, ".")
		if st.Self.KeyExpiry != nil {
			n.keyExpiry = *st.Self.KeyExpiry
		}
	}
	n.certDomains = st.CertDomains
}

// probeStatus queries the node's local API for the live backend state.
// It fails when the node never fully initialized or is being torn down.
func (n *Node) probeStatus(ctx context.Context) (*ipnstate.Status, error) {
	lc, err := n.srv.LocalClient()
	if err != nil {
		return nil, err
	}
	return lc.Status(ctx)
}

// Serve listens on the tailnet-side port and serves h until the listener
// closes. It is intended to run in its own goroutine. Errors caused by a
// concurrent Close are not reported.
func (n *Node) Serve(h http.Handler) error {
	ln, err := n.srv.Listen("tcp", fmt.Sprintf(":%d", n.port))
	if err != nil {
		if n.closing.Load() {
			return nil
		}
		return fmt.Errorf("tailscale listen failed: %w", err)
	}
	n.logger.Info("serving dmanager on tailnet", "port", n.port)

	// Mirror the LAN server's timeout posture (cmd/serve.go).
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := srv.Serve(ln); err != nil && !n.closing.Load() {
		return fmt.Errorf("tailscale serve loop failed: %w", err)
	}
	return nil
}

// forwardedProtoHTTPS forces X-Forwarded-Proto: https on requests that
// arrived over the tailnet TLS listener. tsnet terminates TLS in-process,
// so requests reach the handler stack without the header; auth.secure_cookies
// in auto mode keys off it to set the Secure cookie attribute. Set (not Add)
// overwrites any client-supplied value — on this listener https is the truth.
func forwardedProtoHTTPS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
		h.ServeHTTP(w, r)
	})
}

// ServeHTTPS listens on the tailnet's TLS port (443) and serves h wrapped
// in the X-Forwarded-Proto middleware until the listener closes. It
// requires HTTPS certificates and MagicDNS to be enabled on the tailnet;
// without them, certificate provisioning fails and TLS handshakes time out.
// It is intended to run in its own goroutine. Errors caused by a concurrent
// Close are not reported.
func (n *Node) ServeHTTPS(h http.Handler) error {
	ln, err := n.srv.ListenTLS("tcp", ":443")
	if err != nil {
		if n.closing.Load() {
			return nil
		}
		return fmt.Errorf("tailscale TLS listen failed: %w", err)
	}
	n.logger.Info("serving dmanager on tailnet over HTTPS", "port", 443)

	srv := &http.Server{
		Handler:           forwardedProtoHTTPS(h),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := srv.Serve(ln); err != nil && !n.closing.Load() {
		return fmt.Errorf("tailscale HTTPS serve loop failed: %w", err)
	}
	return nil
}

// Close stops the node and releases all of its resources. It is idempotent;
// repeated calls return nil (unlike the underlying tsnet server, which
// reports net.ErrClosed on double close). Closing a node that never started
// is a no-op: tsnet's Close panics on a server whose backend was never
// initialized.
func (n *Node) Close() error {
	n.closing.Store(true)
	n.closeOnce.Do(func() {
		if n.srv == nil || !n.started.Load() {
			return
		}
		// A failed early startup can still leave tsnet internals half-built;
		// never let a Close panic take down the shutdown path.
		defer func() {
			if r := recover(); r != nil {
				n.closeErr = fmt.Errorf("tsnet close panicked: %v", r)
			}
		}()
		n.closeErr = n.srv.Close()
	})
	return n.closeErr
}
