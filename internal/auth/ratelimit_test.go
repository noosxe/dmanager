package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimiterThresholdAndBackoff(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	user := "targetUser"
	ip := "192.168.1.50"

	// 1..4 failures: not locked out
	for i := 1; i <= 4; i++ {
		dur := rl.RecordFailure(user, ip, now)
		if dur != 0 {
			t.Fatalf("attempt %d should not lock out, got duration %v", i, dur)
		}
		locked, _ := rl.Check(user, ip, now)
		if locked {
			t.Fatalf("attempt %d should not result in Check=locked", i)
		}
	}

	// 5th failure: 1 min lockout
	dur5 := rl.RecordFailure(user, ip, now)
	if dur5 != 1*time.Minute {
		t.Errorf("5th failure expected 1m lockout, got %v", dur5)
	}
	locked, wait := rl.Check(user, ip, now)
	if !locked || wait != 1*time.Minute {
		t.Errorf("expected Check=locked with 1m wait, got locked=%v, wait=%v", locked, wait)
	}

	// 6th failure: 2 min lockout
	dur6 := rl.RecordFailure(user, ip, now)
	if dur6 != 2*time.Minute {
		t.Errorf("6th failure expected 2m lockout, got %v", dur6)
	}

	// 7th failure: 4 min lockout
	dur7 := rl.RecordFailure(user, ip, now)
	if dur7 != 4*time.Minute {
		t.Errorf("7th failure expected 4m lockout, got %v", dur7)
	}

	// 8th failure: 8 min lockout
	dur8 := rl.RecordFailure(user, ip, now)
	if dur8 != 8*time.Minute {
		t.Errorf("8th failure expected 8m lockout, got %v", dur8)
	}

	// 9th failure: 15 min cap (not 16m)
	dur9 := rl.RecordFailure(user, ip, now)
	if dur9 != 15*time.Minute {
		t.Errorf("9th failure expected 15m cap, got %v", dur9)
	}

	// 10th failure: still 15 min cap
	dur10 := rl.RecordFailure(user, ip, now)
	if dur10 != 15*time.Minute {
		t.Errorf("10th failure expected 15m cap, got %v", dur10)
	}
}

func TestRateLimiterSuccessResetsUsernameOnly(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	user := "victim"
	ip := "10.0.0.1"

	// 5 failures
	for i := 1; i <= 5; i++ {
		rl.RecordFailure(user, ip, now)
	}

	locked, _ := rl.Check(user, ip, now)
	if !locked {
		t.Fatalf("expected locked out after 5 failures")
	}

	// Reset username on success
	rl.RecordSuccess(user)

	// User key alone is now unlocked
	userLocked, _ := rl.Check(user, "", now)
	if userLocked {
		t.Errorf("expected user counter to be cleared after success")
	}

	// IP key remains locked to prevent credential stuffing across usernames
	ipLocked, _ := rl.Check("differentUser", ip, now)
	if !ipLocked {
		t.Errorf("expected IP to remain locked after username reset")
	}
}

func TestRateLimiterWindowSlidingAndEviction(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	user := "slidingUser"
	ip := "10.0.0.2"

	// 4 failures at T=0
	for i := 1; i <= 4; i++ {
		rl.RecordFailure(user, ip, now)
	}

	// Move forward 16 minutes (beyond 15m sliding window)
	future := now.Add(16 * time.Minute)

	// Next failure should be treated as 1st failure (not 5th)
	dur := rl.RecordFailure(user, ip, future)
	if dur != 0 {
		t.Errorf("failure after sliding window should not lock out, got %v", dur)
	}

	// Verify Check is clean
	locked, _ := rl.Check(user, ip, future)
	if locked {
		t.Errorf("expected not locked after window slide")
	}
}

func TestExtractClientIP(t *testing.T) {
	const (
		testForwardedIP = "203.0.113.195"
		testLocalIP     = "192.168.1.10"
		xForwardedFor   = "X-Forwarded-For"
	)

	tests := []struct {
		name         string
		reqHeader    http.Header
		remoteAddr   string
		trustedProxy bool
		wantIP       string
	}{
		{
			name:         "remote addr with port without proxy trust",
			reqHeader:    http.Header{xForwardedFor: []string{testForwardedIP}},
			remoteAddr:   "192.168.1.10:54321",
			trustedProxy: false,
			wantIP:       testLocalIP,
		},
		{
			name:         "remote addr without port",
			reqHeader:    nil,
			remoteAddr:   testLocalIP,
			trustedProxy: false,
			wantIP:       testLocalIP,
		},
		{
			name:         "trusted proxy with single X-Forwarded-For",
			reqHeader:    http.Header{xForwardedFor: []string{testForwardedIP}},
			remoteAddr:   "10.0.0.1:8080",
			trustedProxy: true,
			wantIP:       testForwardedIP,
		},
		{
			name:         "trusted proxy with multiple comma-separated X-Forwarded-For",
			reqHeader:    http.Header{xForwardedFor: []string{"203.0.113.195, 70.41.3.18, 150.172.238.178"}},
			remoteAddr:   "10.0.0.1:8080",
			trustedProxy: true,
			wantIP:       testForwardedIP,
		},
		{
			name:         "empty inputs",
			reqHeader:    nil,
			remoteAddr:   "",
			trustedProxy: true,
			wantIP:       "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractClientIP(tc.reqHeader, tc.remoteAddr, tc.trustedProxy)
			if got != tc.wantIP {
				t.Errorf("ExtractClientIP() = %q, want %q", got, tc.wantIP)
			}
		})
	}
}
