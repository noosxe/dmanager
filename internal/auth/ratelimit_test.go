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
		dur := rl.RecordFailure(MethodPassword, user, ip, now)
		if dur != 0 {
			t.Fatalf("attempt %d should not lock out, got duration %v", i, dur)
		}
		locked, _ := rl.Check(MethodPassword, user, ip, now)
		if locked {
			t.Fatalf("attempt %d should not result in Check=locked", i)
		}
	}

	// 5th failure: 1 min lockout
	dur5 := rl.RecordFailure(MethodPassword, user, ip, now)
	if dur5 != 1*time.Minute {
		t.Errorf("5th failure expected 1m lockout, got %v", dur5)
	}
	locked, wait := rl.Check(MethodPassword, user, ip, now)
	if !locked || wait != 1*time.Minute {
		t.Errorf("expected Check=locked with 1m wait, got locked=%v, wait=%v", locked, wait)
	}

	// 6th failure: 2 min lockout
	dur6 := rl.RecordFailure(MethodPassword, user, ip, now)
	if dur6 != 2*time.Minute {
		t.Errorf("6th failure expected 2m lockout, got %v", dur6)
	}

	// 7th failure: 4 min lockout
	dur7 := rl.RecordFailure(MethodPassword, user, ip, now)
	if dur7 != 4*time.Minute {
		t.Errorf("7th failure expected 4m lockout, got %v", dur7)
	}

	// 8th failure: 8 min lockout
	dur8 := rl.RecordFailure(MethodPassword, user, ip, now)
	if dur8 != 8*time.Minute {
		t.Errorf("8th failure expected 8m lockout, got %v", dur8)
	}

	// 9th failure: 15 min cap (not 16m)
	dur9 := rl.RecordFailure(MethodPassword, user, ip, now)
	if dur9 != 15*time.Minute {
		t.Errorf("9th failure expected 15m cap, got %v", dur9)
	}

	// 10th failure: still 15 min cap
	dur10 := rl.RecordFailure(MethodPassword, user, ip, now)
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
		rl.RecordFailure(MethodPassword, user, ip, now)
	}

	locked, _ := rl.Check(MethodPassword, user, ip, now)
	if !locked {
		t.Fatalf("expected locked out after 5 failures")
	}

	// Reset username on success
	rl.RecordSuccess(MethodPassword, user)

	// User key alone is now unlocked
	userLocked, _ := rl.Check(MethodPassword, user, "", now)
	if userLocked {
		t.Errorf("expected user counter to be cleared after success")
	}

	// IP key remains locked to prevent credential stuffing across usernames
	ipLocked, _ := rl.Check(MethodPassword, "differentUser", ip, now)
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
		rl.RecordFailure(MethodPassword, user, ip, now)
	}

	// Move forward 16 minutes (beyond 15m sliding window)
	future := now.Add(16 * time.Minute)

	// Next failure should be treated as 1st failure (not 5th)
	dur := rl.RecordFailure(MethodPassword, user, ip, future)
	if dur != 0 {
		t.Errorf("failure after sliding window should not lock out, got %v", dur)
	}

	// Verify Check is clean
	locked, _ := rl.Check(MethodPassword, user, ip, future)
	if locked {
		t.Errorf("expected not locked after window slide")
	}
}

func TestExtractClientIP(t *testing.T) {
	const (
		xff       = "X-Forwarded-For"
		peer      = "10.0.0.1:8080"
		peerHost  = "10.0.0.1"
		client    = "203.0.113.195"
		localHost = "192.168.1.10"
	)
	hdr := func(lines ...string) http.Header {
		h := http.Header{}
		for _, l := range lines {
			h.Add(xff, l)
		}
		return h
	}

	tests := []struct {
		name       string
		header     http.Header
		remoteAddr string
		trusted    bool
		hops       int
		want       string
	}{
		{"proxy trust off ignores header", hdr(client), localHost + ":54321", false, 1, localHost},
		{"peer without port", nil, localHost, false, 1, localHost},
		{"empty inputs", nil, "", true, 1, ""},
		{"single entry", hdr(client), peer, true, 1, client},
		{"spoofed leftmost entry ignored with one proxy", hdr("1.2.3.4, 203.0.113.195"), peer, true, 1, client},
		{"spoofed chain ignored with one proxy", hdr("1.2.3.4, 5.6.7.8, 203.0.113.195"), peer, true, 1, client},
		{"two hops picks second from right", hdr("1.2.3.4, 203.0.113.195, 198.51.100.7"), peer, true, 2, client},
		{"fewer entries than hops uses leftmost", hdr(client), peer, true, 3, client},
		{"fewer entries than hops with two entries", hdr("203.0.113.195, 198.51.100.7"), peer, true, 5, client},
		{"multiple header lines combined in order", hdr("1.2.3.4, 5.6.7.8", client), peer, true, 1, client},
		{"multiple header lines with two hops", hdr("1.2.3.4", client, "198.51.100.7"), peer, true, 2, client},
		{"spaces trimmed", hdr("  1.2.3.4 ,   203.0.113.195  "), peer, true, 1, client},
		{"ip with port tolerated", hdr("203.0.113.195:4242"), peer, true, 1, client},
		{"ipv6", hdr("2001:db8::1"), peer, true, 1, "2001:db8::1"},
		{"bracketed ipv6 with port", hdr("[2001:db8::1]:4242"), peer, true, 1, "2001:db8::1"},
		{"invalid entry falls back to peer", hdr("1.2.3.4, not-an-ip"), peer, true, 1, peerHost},
		{"empty trailing entry falls back to peer", hdr("1.2.3.4, "), peer, true, 1, peerHost},
		{"empty header value falls back to peer", hdr(""), peer, true, 1, peerHost},
		{"missing header falls back to peer", http.Header{}, peer, true, 1, peerHost},
		{"invalid chosen entry does not fall through to others", hdr("203.0.113.195, garbage, 198.51.100.7"), peer, true, 2, peerHost},
		{"non-positive hops treated as one", hdr("1.2.3.4, 203.0.113.195"), peer, true, 0, client},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractClientIP(tc.header, tc.remoteAddr, tc.trusted, tc.hops)
			if got != tc.want {
				t.Errorf("ExtractClientIP() = %q, want %q", got, tc.want)
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
	if locked, _ := rl.Check(MethodPasskey, "passkey_login", "203.0.113.1", now); locked {
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

func TestLockoutCountersAreSeparatePerMethod(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()

	// Password failures from varying IPs lock the password method only.
	for i := 0; i < RateLimitThreshold; i++ {
		rl.RecordFailure(MethodPassword, "alice", "203.0.113."+string(rune('1'+i)), now)
	}
	if locked, _ := rl.Check(MethodPassword, "alice", "198.51.100.1", now); !locked {
		t.Fatal("password method must be locked after the threshold")
	}
	if locked, _ := rl.Check(MethodPasskey, "alice", "198.51.100.1", now); locked {
		t.Fatal("password failures must not lock passkey sign-in")
	}

	// A passkey success does not reset the password failures.
	rl.RecordSuccess(MethodPasskey, "alice")
	if locked, _ := rl.Check(MethodPassword, "alice", "198.51.100.1", now); !locked {
		t.Fatal("passkey success must not reset the password counter")
	}

	// And a password success does not reset the passkey failures.
	for i := 0; i < RateLimitThreshold; i++ {
		rl.RecordFailure(MethodPasskey, "bob", "198.51.100."+string(rune('1'+i)), now)
	}
	rl.RecordSuccess(MethodPassword, "bob")
	if locked, _ := rl.Check(MethodPasskey, "bob", "192.0.2.1", now); !locked {
		t.Fatal("password success must not reset the passkey counter")
	}
}

func TestIPLockoutAppliesAcrossMethods(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	ip := "203.0.113.9"
	for i := 0; i < RateLimitThreshold; i++ {
		rl.RecordFailure(MethodPassword, "user"+string(rune('a'+i)), ip, now)
	}
	if locked, _ := rl.Check(MethodPasskey, "someone", ip, now); !locked {
		t.Fatal("per-IP lockout must apply to passkey sign-in too")
	}
}

func TestPreUserPasskeyFailuresCountPerIPOnly(t *testing.T) {
	rl := NewRateLimiter(false)
	now := time.Now()
	ipA, ipB := "203.0.113.10", "203.0.113.11"

	// Failures before the user is known (bad challenge, unknown credential)
	// are recorded with an empty username, so only the IP counts them.
	for i := 0; i < RateLimitThreshold; i++ {
		rl.RecordFailure(MethodPasskey, "", ipA, now)
	}
	if locked, _ := rl.Check(MethodPasskey, "", ipA, now); !locked {
		t.Fatal("the failing IP must be locked")
	}
	if locked, _ := rl.Check(MethodPasskey, "", ipB, now); locked {
		t.Fatal("another IP must not be locked by pre-user failures")
	}
	// A real user, including one named like the old shared key, is unaffected.
	if locked, _ := rl.Check(MethodPasskey, "passkey_login", ipB, now); locked {
		t.Fatal("pre-user failures must not be recorded under a username")
	}
}
