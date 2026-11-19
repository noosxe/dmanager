package tailscale

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// upAttemptTimeout bounds one tsnet.Up attempt. Hitting it is not a
	// failure: the node keeps working in the background and the next attempt
	// simply resumes watching it. The bound exists so that the node state is
	// logged regularly while the node waits (for example for device approval).
	upAttemptTimeout = 60 * time.Second
	// backoffInitial and backoffMax bound the delay between attempts that
	// failed with a transient error; the delay doubles after each failure.
	backoffInitial = 2 * time.Second
	backoffMax     = 60 * time.Second
)

// terminalError marks a start failure that retrying cannot fix.
type terminalError struct{ err error }

func (e *terminalError) Error() string { return e.err.Error() }
func (e *terminalError) Unwrap() error { return e.err }

// IsTerminal reports whether err is a start failure that cannot recover, such
// as an invalid, expired or revoked auth key, or an unusable state directory.
func IsTerminal(err error) bool {
	var t *terminalError
	return errors.As(err, &t)
}

// terminalKeyPhrases are fragments of the control server's rejection messages
// (surfaced by tsnet.Up as "backend: <message>") that mean the auth key itself
// is unusable. Any other backend message is treated as transient.
var terminalKeyPhrases = []string{
	"invalid key",
	"key expired",
	"key has expired",
	"expired key",
	"revoked",
	"invalid auth key",
	"auth key not valid",
	"not valid",
}

// classifyUpError wraps err as terminal when it reports an unusable auth key.
func classifyUpError(err error) error {
	if err == nil || IsTerminal(err) {
		return err
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "backend:") {
		return err
	}
	for _, p := range terminalKeyPhrases {
		if strings.Contains(msg, p) {
			return &terminalError{err: fmt.Errorf("tailscale auth key rejected, fix tailscale.auth_key and restart: %w", err)}
		}
	}
	return err
}

// describeBackendState turns an ipn backend state into an operator hint.
func describeBackendState(state string) string {
	switch state {
	case "NeedsMachineAuth":
		return "waiting for the device to be approved in the tailnet admin console"
	case "NeedsLogin":
		return "waiting for the control server to accept the login"
	case "Starting":
		return "connecting to the tailnet"
	case "":
		return "state unknown"
	default:
		return state
	}
}

// backoff returns the next delay after d, doubling up to backoffMax.
func nextBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return backoffInitial
	}
	d *= 2
	if d > backoffMax {
		return backoffMax
	}
	return d
}

// retryLoop calls attempt until it succeeds, returns a terminal error, or ctx
// ends. An attempt that fails only because its own deadline passed
// (context.DeadlineExceeded while ctx is alive) is retried at once; any other
// error is retried after a capped exponential delay. onRetry is called before
// each retry with the failed attempt's error and the delay about to be waited.
// sleep waits for d or until ctx ends; it is a parameter so tests can fake the
// clock.
func retryLoop(
	ctx context.Context,
	attempt func(ctx context.Context) error,
	onRetry func(n int, err error, delay time.Duration),
	sleep func(ctx context.Context, d time.Duration) error,
) error {
	var delay time.Duration
	for n := 1; ; n++ {
		err := attempt(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if IsTerminal(err) {
			return err
		}
		wait := time.Duration(0)
		if !errors.Is(err, context.DeadlineExceeded) {
			delay = nextBackoff(delay)
			wait = delay
		}
		onRetry(n, err, wait)
		if wait > 0 {
			if serr := sleep(ctx, wait); serr != nil {
				return serr
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Connect brings the node up, retrying in the background until it is running.
// It returns nil once the node is running, a terminal error (see IsTerminal)
// when retrying cannot help, or ctx's error when ctx ends. Transient failures
// and timeouts never end it; the node state is logged on every retry and is
// visible through Snapshot while it waits.
func (n *Node) Connect(ctx context.Context) error {
	return n.connect(ctx, n.startAttempt, sleepCtx)
}

func (n *Node) startAttempt(ctx context.Context) error {
	attemptCtx, cancel := context.WithTimeout(ctx, upAttemptTimeout)
	defer cancel()
	err := n.Start(attemptCtx)
	if err != nil && !IsTerminal(err) && attemptCtx.Err() != nil && ctx.Err() == nil {
		// The attempt's own deadline passed; however tsnet wrapped it, it is
		// just "still waiting".
		return context.DeadlineExceeded
	}
	return err
}

func (n *Node) connect(
	ctx context.Context,
	attempt func(ctx context.Context) error,
	sleep func(ctx context.Context, d time.Duration) error,
) error {
	return retryLoop(ctx, attempt, func(num int, err error, delay time.Duration) {
		state := n.liveBackendState(ctx)
		n.noteWaiting(err)
		attrs := []any{"attempt", num, "state", state, "status", describeBackendState(state)}
		if !errors.Is(err, context.DeadlineExceeded) {
			attrs = append(attrs, "error", err, "retry_in", delay.String())
		}
		n.logger.Warn("tailscale node not up yet; retrying", attrs...)
	}, sleep)
}

// liveBackendState probes the backend state for logging; empty when the node
// cannot be queried.
func (n *Node) liveBackendState(ctx context.Context) string {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := n.probeStatus(pctx)
	if err != nil {
		return ""
	}
	n.cacheStatus(st)
	return st.BackendState
}

// noteWaiting records that the node is still coming up: the last transient
// failure is kept for Snapshot, but the node is not marked failed.
func (n *Node) noteWaiting(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.startErr = nil
	if errors.Is(err, context.DeadlineExceeded) {
		n.waitDetail = ""
		return
	}
	n.waitDetail = err.Error()
}
