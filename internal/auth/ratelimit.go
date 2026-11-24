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
)

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
	beginLastSweep time.Time
}

func NewRateLimiter(trustedProxy bool) *RateLimiter {
	return &RateLimiter{
		beginHits:    make(map[string][]time.Time),
		userRecords:  make(map[string]*attemptRecord),
		ipRecords:    make(map[string]*attemptRecord),
		window:       RateLimitWindow,
		threshold:    RateLimitThreshold,
		maxLockout:   RateLimitMaxLockout,
		baseLockout:  RateLimitBaseLockout,
		trustedProxy: trustedProxy,
	}
}

// AllowBegin records a passkey login begin call from ip and reports whether it
// is within BeginLimit calls per BeginWindow (sliding). When it is not, the
// call is not recorded and retryAfter says when the oldest hit leaves the
// window. It is independent of the failure lockout, so a flood of begins
// never locks anyone out of signing in, it only slows the flooder.
func (rl *RateLimiter) AllowBegin(ip string, now time.Time) (allowed bool, retryAfter time.Duration) {
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
		rl.beginLastSweep = now
	}

	hits := rl.beginHits[ip][:0]
	for _, t := range rl.beginHits[ip] {
		if t.After(windowStart) {
			hits = append(hits, t)
		}
	}
	if len(hits) >= BeginLimit {
		rl.beginHits[ip] = hits
		return false, hits[0].Add(BeginWindow).Sub(now)
	}
	rl.beginHits[ip] = append(hits, now)
	return true, 0
}

// Check verifies if either the username or client IP is currently rate-limited.
// Returns (locked bool, retryAfter time.Duration).
func (rl *RateLimiter) Check(username, ip string, now time.Time) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.evictExpired(now)

	userWait := rl.checkKey(rl.userRecords, username, now)
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

// RecordFailure records a failed login attempt for the given username and IP.
// Returns the new retry duration if this failure resulted in a lockout.
func (rl *RateLimiter) RecordFailure(username, ip string, now time.Time) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.evictExpired(now)

	userWait := rl.recordFailureKey(rl.userRecords, username, now)
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

// RecordSuccess resets the failure counter for the given username.
func (rl *RateLimiter) RecordSuccess(username string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.userRecords, username)
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

// ExtractClientIP extracts the client IP address from the request header or remote address.
func ExtractClientIP(reqHeader http.Header, remoteAddr string, trustedProxy bool) string {
	if trustedProxy && reqHeader != nil {
		xff := reqHeader.Get("X-Forwarded-For")
		if xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				clientIP := strings.TrimSpace(parts[0])
				if clientIP != "" {
					return clientIP
				}
			}
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
