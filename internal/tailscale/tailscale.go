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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tailscale.com/tsnet"

	"dmanager/internal/config"
)

// Node owns the embedded tsnet lifecycle for one dmanager process.
type Node struct {
	srv    *tsnet.Server
	port   int
	logger *slog.Logger

	closing   atomic.Bool
	started   atomic.Bool
	closeOnce sync.Once
	closeErr  error
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
		srv:    srv,
		port:   cfg.Port,
		logger: logger,
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
		return fmt.Errorf("tailscale node failed to connect: %w", err)
	}

	ips := make([]string, 0, len(status.TailscaleIPs))
	for _, ip := range status.TailscaleIPs {
		ips = append(ips, ip.String())
	}
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
