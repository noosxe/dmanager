package auth

import (
	"fmt"
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

func TestAllowBeginSlidingWindowPerIP(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()

	for i := 0; i < BeginLimit; i++ {
		if ok, _, _ := rl.AllowBegin("203.0.113.1", now); !ok {
			t.Fatalf("begin %d of %d must be allowed", i+1, BeginLimit)
		}
	}
	ok, wait, first := rl.AllowBegin("203.0.113.1", now)
	if ok {
		t.Fatalf("begin %d within the window must be refused", BeginLimit+1)
	}
	if !first {
		t.Error("the first refusal must be reported for auditing")
	}
	if _, _, again := rl.AllowBegin("203.0.113.1", now); again {
		t.Error("only one refusal per window may be reported")
	}
	if wait <= 0 || wait > BeginWindow {
		t.Errorf("retry-after must be within the window, got %v", wait)
	}

	// Another IP is unaffected, and the first one recovers after the window.
	if ok, _, _ := rl.AllowBegin("203.0.113.2", now); !ok {
		t.Error("a different IP must not share the budget")
	}
	if ok, _, _ := rl.AllowBegin("203.0.113.1", now.Add(BeginWindow+time.Second)); !ok {
		t.Error("budget must recover after the window")
	}

	// Begin flooding never feeds the failure lockout.
	if locked, _ := rl.Check("passkey_login", "203.0.113.1", now); locked {
		t.Error("refused begins must not lock out sign-in")
	}
}

func TestAllowBeginBoundsTrackedIPs(t *testing.T) {
	rl := NewRateLimiter(false)
	rl.beginMaxKeys = 3
	now := time.Now()

	for _, ip := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		if ok, _, _ := rl.AllowBegin(ip, now); !ok {
			t.Fatalf("%s must be allowed", ip)
		}
	}
	// New IPs beyond the cap share one overflow key with its own budget.
	for i := 0; i < BeginLimit; i++ {
		if ok, _, _ := rl.AllowBegin(fmt.Sprintf("192.0.2.%d", i), now); !ok {
			t.Fatalf("overflow call %d within the shared budget must be allowed", i)
		}
	}
	if ok, _, _ := rl.AllowBegin("192.0.2.200", now); ok {
		t.Error("the shared overflow key must be limited")
	}
	if len(rl.beginHits) > 4 || len(rl.beginNotified) > 4 {
		t.Errorf("tracked keys must stay within the cap (+ overflow), got %d/%d", len(rl.beginHits), len(rl.beginNotified))
	}
	// A tracked IP keeps its own budget.
	if ok, _, _ := rl.AllowBegin("198.51.100.1", now); !ok {
		t.Error("an already tracked IP must keep its own budget")
	}
}

func TestAllowBeginCapsAuditedRefusals(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	audited := 0
	// Many distinct IPs, each pushed over its limit once.
	for i := 0; i < 200; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/250, i%250)
		for j := 0; j < BeginLimit; j++ {
			rl.AllowBegin(ip, now)
		}
		if _, _, first := rl.AllowBegin(ip, now); first {
			audited++
		}
	}
	if audited != BeginAuditPerWindow {
		t.Errorf("expected %d audited refusals in total, got %d", BeginAuditPerWindow, audited)
	}
	// The budget returns with the next window.
	later := now.Add(BeginWindow + time.Second)
	for j := 0; j < BeginLimit; j++ {
		rl.AllowBegin("10.9.9.9", later)
	}
	if _, _, first := rl.AllowBegin("10.9.9.9", later); !first {
		t.Error("the audit budget must recover in the next window")
	}
}
