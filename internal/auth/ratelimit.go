package auth

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	RateLimitWindow      = 15 * time.Minute
	RateLimitThreshold   = 5
	RateLimitMaxLockout  = 15 * time.Minute
	RateLimitBaseLockout = 1 * time.Minute

	// BeginLimit is how many passkey login begin calls one IP may make per
	// BeginWindow. Begin is unauthenticated and writes a challenge row each
	// time, so it needs its own limit: it has no failure to count.
	BeginLimit  = 20
	BeginWindow = time.Minute

	// BeginMaxTrackedIPs bounds the begin throttle's memory. Once that many
	// IPs are tracked, new ones share one overflow key, which is limited like
	// any other IP, so memory does not grow with the request rate or with
	// spoofed or rotating addresses.
	BeginMaxTrackedIPs = 10000
	// BeginAuditPerWindow caps the rate_limited audit rows the begin throttle
	// may write in total per BeginWindow, so rotating addresses cannot grow
	// the auth event log either.
	BeginAuditPerWindow = 30

	beginOverflowKey = "overflow"
)

// Sign-in methods. Per-username lockout records are kept per method, so
// failures with one method never lock out or reset another. Per-IP records
// are shared across methods.
const (
	MethodPassword = "password"
	MethodPasskey  = "passkey"
)

func userKey(method, username string) string {
	if username == "" {
		return ""
	}
	return method + ":" + username
}

type attemptRecord struct {
	failures    []time.Time
	lockedUntil time.Time
}

type RateLimiter struct {
	mu           sync.Mutex
	userRecords  map[string]*attemptRecord
	ipRecords    map[string]*attemptRecord
	window       time.Duration
	threshold    int
	maxLockout   time.Duration
	baseLockout  time.Duration
	trustedProxy bool

	beginHits      map[string][]time.Time
	beginNotified  map[string]time.Time
	beginLastSweep time.Time
	beginMaxKeys   int
	beginAuditMax  int
	beginAuditFrom time.Time
	beginAuditUsed int
}

func NewRateLimiter(trustedProxy bool) *RateLimiter {
	return &RateLimiter{
		beginHits:     make(map[string][]time.Time),
		beginNotified: make(map[string]time.Time),
		beginMaxKeys:  BeginMaxTrackedIPs,
		beginAuditMax: BeginAuditPerWindow,
		userRecords:   make(map[string]*attemptRecord),
		ipRecords:     make(map[string]*attemptRecord),
		window:        RateLimitWindow,
		threshold:     RateLimitThreshold,
		maxLockout:    RateLimitMaxLockout,
		baseLockout:   RateLimitBaseLockout,
		trustedProxy:  trustedProxy,
	}
}

// AllowBegin records a passkey login begin call from ip and reports whether it
// is within BeginLimit calls per BeginWindow (sliding). When it is not, the
// call is not recorded and retryAfter says when the oldest hit leaves the
// window. first is true for only one refusal per IP per window, and for at
// most BeginAuditPerWindow refusals in total, so callers can audit the
// throttle without letting a flood (even from rotating addresses) grow the
// audit log. It is independent of the failure lockout, so a flood of begins
// never locks anyone out of signing in, it only slows the flooder.
func (rl *RateLimiter) AllowBegin(ip string, now time.Time) (allowed bool, retryAfter time.Duration, first bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	windowStart := now.Add(-BeginWindow)

	// Drop idle IPs at most once per window so the map stays bounded.
	if now.Sub(rl.beginLastSweep) >= BeginWindow {
		for k, hits := range rl.beginHits {
			if len(hits) == 0 || !hits[len(hits)-1].After(windowStart) {
				delete(rl.beginHits, k)
			}
		}
		for k, t := range rl.beginNotified {
			if !t.After(windowStart) {
				delete(rl.beginNotified, k)
			}
		}
		rl.beginLastSweep = now
	}

	if _, tracked := rl.beginHits[ip]; !tracked && len(rl.beginHits) >= rl.beginMaxKeys {
		ip = beginOverflowKey
	}

	hits := rl.beginHits[ip][:0]
	for _, t := range rl.beginHits[ip] {
		if t.After(windowStart) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= BeginLimit {
		rl.beginHits[ip] = hits
		first = !rl.beginNotified[ip].After(windowStart)
		if first {
			rl.beginNotified[ip] = now
			first = rl.takeBeginAudit(now)
		}
		return false, hits[0].Add(BeginWindow).Sub(now), first
	}
	rl.beginHits[ip] = append(hits, now)
	return true, 0, false
}

// takeBeginAudit spends one of the begin throttle's audit rows for this
// window, if any are left. The caller holds rl.mu.
func (rl *RateLimiter) takeBeginAudit(now time.Time) bool {
	if now.Sub(rl.beginAuditFrom) >= BeginWindow {
		rl.beginAuditFrom = now
		rl.beginAuditUsed = 0
	}
	if rl.beginAuditUsed >= rl.beginAuditMax {
		return false
	}
	rl.beginAuditUsed++
	return true
}

// Check verifies if either the username (for the given method) or client IP is
// currently rate-limited. Returns (locked bool, retryAfter time.Duration).
func (rl *RateLimiter) Check(method, username, ip string, now time.Time) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.evictExpired(now)

	userWait := rl.checkKey(rl.userRecords, userKey(method, username), now)
	ipWait := rl.checkKey(rl.ipRecords, ip, now)

	maxWait := userWait
	if ipWait > maxWait {
		maxWait = ipWait
	}

	if maxWait > 0 {
		return true, maxWait
	}
	return false, 0
}

func (rl *RateLimiter) checkKey(records map[string]*attemptRecord, key string, now time.Time) time.Duration {
	if key == "" {
		return 0
	}
	rec, exists := records[key]
	if !exists {
		return 0
	}

	if now.Before(rec.lockedUntil) {
		return rec.lockedUntil.Sub(now)
	}
	return 0
}

// RecordFailure records a failed login attempt for the given method, username and IP.
// Returns the new retry duration if this failure resulted in a lockout.
func (rl *RateLimiter) RecordFailure(method, username, ip string, now time.Time) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.evictExpired(now)

	userWait := rl.recordFailureKey(rl.userRecords, userKey(method, username), now)
	ipWait := rl.recordFailureKey(rl.ipRecords, ip, now)

	maxWait := userWait
	if ipWait > maxWait {
		maxWait = ipWait
	}
	return maxWait
}

func (rl *RateLimiter) recordFailureKey(records map[string]*attemptRecord, key string, now time.Time) time.Duration {
	if key == "" {
		return 0
	}
	rec, exists := records[key]
	if !exists {
		rec = &attemptRecord{}
		records[key] = rec
	}

	// Prune failures outside the sliding window
	windowStart := now.Add(-rl.window)
	validFailures := rec.failures[:0]
	for _, t := range rec.failures {
		if t.After(windowStart) {
			validFailures = append(validFailures, t)
		}
	}
	rec.failures = append(validFailures, now)

	count := len(rec.failures)
	if count >= rl.threshold {
		// Calculate exponential backoff
		// 5 failures -> step 0 -> 1 min
		// 6 failures -> step 1 -> 2 min
		// 7 failures -> step 2 -> 4 min
		step := count - rl.threshold
		backoffMultiplier := math.Pow(2, float64(step))
		lockoutDuration := time.Duration(float64(rl.baseLockout) * backoffMultiplier)
		if lockoutDuration > rl.maxLockout {
			lockoutDuration = rl.maxLockout
		}

		rec.lockedUntil = now.Add(lockoutDuration)
		return lockoutDuration
	}

	return 0
}

// RecordSuccess resets the failure counter for the given username and method
// only; the other method's counter and the IP records are left alone.
func (rl *RateLimiter) RecordSuccess(method, username string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.userRecords, userKey(method, username))
}

func (rl *RateLimiter) evictExpired(now time.Time) {
	windowStart := now.Add(-rl.window)

	for k, rec := range rl.userRecords {
		if now.After(rec.lockedUntil) {
			hasRecent := false
			for _, t := range rec.failures {
				if t.After(windowStart) {
					hasRecent = true
					break
				}
			}
			if !hasRecent {
				delete(rl.userRecords, k)
			}
		}
	}

	for k, rec := range rl.ipRecords {
		if now.After(rec.lockedUntil) {
			hasRecent := false
			for _, t := range rec.failures {
				if t.After(windowStart) {
					hasRecent = true
					break
				}
			}
			if !hasRecent {
				delete(rl.ipRecords, k)
			}
		}
	}
}

// ExtractClientIP returns the client address for rate limiting and auditing.
//
// Without trustedProxy it is the TCP peer address and X-Forwarded-For is
// ignored. With it, X-Forwarded-For is read from the right: every proxy
// appends the address it received the request from, so only the entries added
// by the operator's own proxies can be trusted and anything further left is
// client-controlled. The entry trustedHops positions from the right is used
// (1 is the rightmost entry, the address the nearest proxy saw). If the header
// has fewer entries, the leftmost one is used. Multiple header lines are
// combined in order. When the chosen entry is not a valid IP address the peer
// address is returned.
func ExtractClientIP(reqHeader http.Header, remoteAddr string, trustedProxy bool, trustedHops int) string {
	if trustedProxy && reqHeader != nil {
		if ip := forwardedClientIP(reqHeader.Values("X-Forwarded-For"), trustedHops); ip != "" {
			return ip
		}
	}

	if remoteAddr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

// forwardedClientIP picks the entry hops from the right of the combined
// X-Forwarded-For lines and returns it as a canonical IP, or "" if there is no
// usable entry.
func forwardedClientIP(lines []string, hops int) string {
	if hops < 1 {
		hops = 1
	}
	var entries []string
	for _, line := range lines {
		entries = append(entries, strings.Split(line, ",")...)
	}
	if len(entries) == 0 {
		return ""
	}
	idx := len(entries) - hops
	if idx < 0 {
		idx = 0
	}
	entry := strings.TrimSpace(entries[idx])
	if ip := net.ParseIP(entry); ip != nil {
		return ip.String()
	}
	// Tolerate "ip:port" and "[ipv6]:port".
	if host, _, err := net.SplitHostPort(entry); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
	}
	return ""
}
