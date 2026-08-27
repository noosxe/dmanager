package auth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	_ "github.com/ncruces/go-sqlite3/driver"
	"golang.org/x/crypto/bcrypt"

	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/version"
)

const (
	testAdminUsername  = "admin"
	testPassword       = "password12345"
	wrongPassword      = "wrongpassword"
	eventLoginSuccess  = "login_success"
	testBobUsername    = "bob"
	testViewerRole     = "viewer"
	challengeRegTest   = "registration"
	challengeLoginTest = "login"
)

func newTestDB(t *testing.T) *db.Queries {
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return db.New(dbConn)
}

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		SessionIdleTimeout:        168 * time.Hour,
		SessionAbsoluteTimeout:    720 * time.Hour,
		RememberMeIdleTimeout:     720 * time.Hour,
		RememberMeAbsoluteTimeout: 2160 * time.Hour,
		SecureCookies:             config.SecureCookiesAuto,
		BcryptCost:                12,
	}
}

func testWebAuthnConfig() config.WebAuthnConfig {
	return config.WebAuthnConfig{
		RPID:                    "localhost",
		Origins:                 []string{"http://localhost:9283", "https://localhost:9283"},
		RequireUserVerification: "preferred",
	}
}

func TestGetServerStatus(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	resp, err := svc.GetServerStatus(ctx, connect.NewRequest(&v1.GetServerStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.Version != version.Version || resp.Msg.Commit != version.Commit || resp.Msg.BuildDate != version.Date {
		t.Errorf("expected build metadata (version=%q commit=%q date=%q), got (version=%q commit=%q date=%q)",
			version.Version, version.Commit, version.Date, resp.Msg.Version, resp.Msg.Commit, resp.Msg.BuildDate)
	}
	if !resp.Msg.NeedsSetup {
		t.Errorf("expected NeedsSetup to be true, got false")
	}

	_, err = queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "test",
		PasswordHash: "hash",
		Role:         adminRole,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	resp, err = svc.GetServerStatus(ctx, connect.NewRequest(&v1.GetServerStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.NeedsSetup {
		t.Errorf("expected NeedsSetup to be false, got true")
	}
}

func TestSetupAdmin(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	resp, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.Username != testAdminUsername || resp.Msg.Role != adminRole {
		t.Errorf("unexpected response: %+v", resp.Msg)
	}

	// Verify bcrypt cost 12
	user, err := queries.GetUserByUsername(ctx, testAdminUsername)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(user.PasswordHash))
	if err != nil {
		t.Fatalf("failed to get bcrypt cost: %v", err)
	}
	if cost != 12 {
		t.Errorf("expected bcrypt cost 12, got %d", cost)
	}

	_, err = svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: "admin2",
		Password: testPassword,
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok || connectErr.Code() != connect.CodeFailedPrecondition {
		t.Errorf("expected FailedPrecondition error, got %v", err)
	}
}

func TestLogin(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Standard login (remember_me = false)
	resp, err := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username:   testAdminUsername,
		Password:   testPassword,
		RememberMe: false,
	}))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if resp.Msg.Username != testAdminUsername || resp.Msg.Role != adminRole {
		t.Errorf("unexpected login response: %+v", resp.Msg)
	}

	cookie := resp.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "session_id=") {
		t.Errorf("expected Set-Cookie with session_id, got %q", cookie)
	}
	expectedMaxAge := int((168 * time.Hour).Seconds())
	if !strings.Contains(cookie, "Max-Age=604800") {
		t.Errorf("expected Max-Age=%d in cookie, got %q", expectedMaxAge, cookie)
	}

	// Verify session row in DB has correct idle and absolute timeouts
	sessionID := parseSessionCookie(cookie)
	sessionRow, err := queries.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to query session: %v", err)
	}
	if sessionRow.ExpiresAt.Before(time.Now().Add(167 * time.Hour)) {
		t.Errorf("unexpected expires_at: %v", sessionRow.ExpiresAt)
	}
	if sessionRow.AbsoluteExpiresAt.Before(time.Now().Add(719 * time.Hour)) {
		t.Errorf("unexpected absolute_expires_at: %v", sessionRow.AbsoluteExpiresAt)
	}

	// Remember-me login
	reqRemember := connect.NewRequest(&v1.LoginRequest{
		Username:   testAdminUsername,
		Password:   testPassword,
		RememberMe: true,
	})
	respRemember, err := svc.Login(ctx, reqRemember)
	if err != nil {
		t.Fatalf("login with remember_me failed: %v", err)
	}
	cookieRemember := respRemember.Header().Get("Set-Cookie")
	expectedRememberMaxAge := int((720 * time.Hour).Seconds())
	if !strings.Contains(cookieRemember, "Max-Age=2592000") {
		t.Errorf("expected Max-Age=%d in remember-me cookie, got %q", expectedRememberMaxAge, cookieRemember)
	}
	sessionIDRemember := parseSessionCookie(cookieRemember)
	sessionRowRemember, err := queries.GetSession(ctx, sessionIDRemember)
	if err != nil {
		t.Fatalf("failed to query session: %v", err)
	}
	if sessionRowRemember.ExpiresAt.Before(time.Now().Add(719 * time.Hour)) {
		t.Errorf("unexpected remember-me expires_at: %v", sessionRowRemember.ExpiresAt)
	}
	if sessionRowRemember.AbsoluteExpiresAt.Before(time.Now().Add(2159 * time.Hour)) {
		t.Errorf("unexpected remember-me absolute_expires_at: %v", sessionRowRemember.AbsoluteExpiresAt)
	}

	// Wrong password
	_, err = svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: wrongPassword,
	}))
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok || connectErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestLoginLegacyBcryptCost10(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// Seed user with legacy cost 10 hash
	hash10, err := bcrypt.GenerateFromPassword([]byte("legacyPass123"), 10)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	const legacyUsername = "legacyuser"
	const legacyRole = "viewer"

	_, err = queries.CreateUser(ctx, db.CreateUserParams{
		Username:     legacyUsername,
		PasswordHash: string(hash10),
		Role:         legacyRole,
	})
	if err != nil {
		t.Fatalf("failed to seed legacy user: %v", err)
	}

	// Attempt login
	resp, err := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: legacyUsername,
		Password: "legacyPass123",
	}))
	if err != nil {
		t.Fatalf("legacy login failed: %v", err)
	}
	if resp.Msg.Username != legacyUsername || resp.Msg.Role != legacyRole {
		t.Errorf("unexpected response: %+v", resp.Msg)
	}
}

func TestCookieSecureMatrix(t *testing.T) {
	const xForwardedProto = "X-Forwarded-Proto"
	const protoHTTPS = "https"
	tests := []struct {
		name          string
		secureCookies string
		reqHeader     http.Header
		wantSecure    bool
	}{
		{
			name:          "auto mode without https header",
			secureCookies: config.SecureCookiesAuto,
			reqHeader:     http.Header{},
			wantSecure:    false,
		},
		{
			name:          "auto mode with https header",
			secureCookies: config.SecureCookiesAuto,
			reqHeader:     http.Header{xForwardedProto: []string{protoHTTPS}},
			wantSecure:    true,
		},
		{
			name:          "always mode without https header",
			secureCookies: config.SecureCookiesAlways,
			reqHeader:     http.Header{},
			wantSecure:    true,
		},
		{
			name:          "always mode with https header",
			secureCookies: config.SecureCookiesAlways,
			reqHeader:     http.Header{xForwardedProto: []string{protoHTTPS}},
			wantSecure:    true,
		},
		{
			name:          "never mode with https header",
			secureCookies: config.SecureCookiesNever,
			reqHeader:     http.Header{xForwardedProto: []string{protoHTTPS}},
			wantSecure:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cookie := formatSessionCookie("test-session-123", 3600, tt.secureCookies, tt.reqHeader)
			hasSecure := strings.Contains(cookie, "; Secure")
			if hasSecure != tt.wantSecure {
				t.Errorf("formatSessionCookie() secure = %v, want %v (cookie: %s)", hasSecure, tt.wantSecure, cookie)
			}
			if !strings.Contains(cookie, "Max-Age=3600") {
				t.Errorf("expected Max-Age=3600, got %s", cookie)
			}
			if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || !strings.Contains(cookie, "Path=/") {
				t.Errorf("cookie missing expected attributes: %s", cookie)
			}
		})
	}
}

func TestGetMe(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)

	user := db.User{
		ID:       42,
		Username: testBobUsername,
		Role:     "user",
	}

	ctx := WithUser(context.Background(), user)
	resp, err := svc.GetMe(ctx, connect.NewRequest(&v1.GetMeRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.UserId != 42 || resp.Msg.Username != testBobUsername || resp.Msg.Role != "user" {
		t.Errorf("unexpected response: %+v", resp.Msg)
	}

	_, err = svc.GetMe(context.Background(), connect.NewRequest(&v1.GetMeRequest{}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	connectErr, ok := err.(*connect.Error)
	if !ok || connectErr.Code() != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated error, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	loginResp, err := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	cookie := loginResp.Header().Get("Set-Cookie")
	sessionID := parseSessionCookie(cookie)

	ctx = WithSessionID(ctx, sessionID)
	req := connect.NewRequest(&v1.LogoutRequest{})
	req.Header().Set("Cookie", cookie)

	logoutResp, err := svc.Logout(ctx, req)
	if err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	deletedCookie := logoutResp.Header().Get("Set-Cookie")
	if !strings.Contains(deletedCookie, "Max-Age=0") {
		t.Errorf("expected Max-Age=0 in deleted cookie, got %q", deletedCookie)
	}

	_, err = queries.GetSession(context.Background(), sessionID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected session to be deleted, queries.GetSession returned: %v", err)
	}
}

func TestSetupAdminPasswordPolicyRejection(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// Less than 12 chars should be rejected
	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: "short11char",
	}))
	if err == nil {
		t.Fatalf("expected password < 12 chars to be rejected, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", err)
	}
}

func TestLoginRateLimitingThrottling(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// 5 failed login attempts
	for i := 1; i <= 5; i++ {
		_, loginErr := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
			Username: testAdminUsername,
			Password: "wrongpassword",
		}))
		if loginErr == nil {
			t.Fatalf("attempt %d: expected login to fail", i)
		}
		if connect.CodeOf(loginErr) != connect.CodeUnauthenticated {
			t.Errorf("attempt %d: expected CodeUnauthenticated, got %v", i, connect.CodeOf(loginErr))
		}
	}

	// 6th attempt should be blocked by rate limiter with CodeResourceExhausted
	_, err = svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: testPassword, // Even with correct password, account/IP is locked out
	}))
	if err == nil {
		t.Fatalf("expected 6th attempt to be blocked by rate limiter, got nil")
	}
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("expected CodeResourceExhausted, got %v (%v)", connect.CodeOf(err), err)
	}
}

func TestLoginTimingEqualization(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	if len(svc.dummyHash) == 0 {
		t.Fatalf("expected dummyHash to be initialized in Service")
	}

	// Non-existent user login executes dummy compare without panic/error crash
	_, err := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: "nonexistentuser",
		Password: "randompassword123",
	}))
	if err == nil {
		t.Fatalf("expected non-existent user to fail")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %v", err)
	}
}

func TestAuthEventsLoggingOnDecisionPoints(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// 1. Setup Admin writes setup_admin event
	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("setup admin failed: %v", err)
	}

	// 2. Failed login writes login_failed event
	_, err = svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: wrongPassword,
	}))
	if err == nil {
		t.Fatalf("expected login to fail")
	}

	// 3. Successful login writes login_success event
	loginReq := connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: testPassword,
	})
	loginReq.Header().Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	loginResp, err := svc.Login(ctx, loginReq)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// 4. Logout writes logout event
	cookie := loginResp.Header().Get("Set-Cookie")
	sessionID := parseSessionCookie(cookie)
	user, _ := queries.GetUserByUsername(ctx, testAdminUsername)
	authedCtx := WithUser(WithSessionID(ctx, sessionID), user)

	logoutReq := connect.NewRequest(&v1.LogoutRequest{})
	logoutReq.Header().Set("Cookie", cookie)
	_, err = svc.Logout(authedCtx, logoutReq)
	if err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	// Verify all events recorded in DB
	events, err := queries.ListAuthEvents(ctx, db.ListAuthEventsParams{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("failed to list auth events: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// Verify no passwords, tokens, or session IDs leaked in event details
	forbiddenSubstrings := []string{testPassword, sessionID, "Bearer", "token="}
	for _, e := range events {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(e.Detail, forbidden) {
				t.Errorf("event detail %q leaked sensitive secret %q", e.Detail, forbidden)
			}
		}
	}
}

func TestListAuthEventsScoping(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// Create admin and viewer users
	admin, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "adminuser",
		PasswordHash: testDummyHash,
		Role:         adminRole,
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	viewer, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "vieweruser",
		PasswordHash: testDummyHash,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create viewer: %v", err)
	}

	// Record events for both
	_, _ = queries.CreateAuthEvent(ctx, db.CreateAuthEventParams{
		UserID:   sql.NullInt64{Int64: admin.ID, Valid: true},
		Username: admin.Username,
		Event:    eventLoginSuccess,
		Detail:   "admin login",
	})
	_, _ = queries.CreateAuthEvent(ctx, db.CreateAuthEventParams{
		UserID:   sql.NullInt64{Int64: viewer.ID, Valid: true},
		Username: viewer.Username,
		Event:    eventLoginSuccess,
		Detail:   "viewer login",
	})

	// Viewer requests events -> sees only own event
	viewerCtx := WithUser(ctx, viewer)
	viewerResp, err := svc.ListAuthEvents(viewerCtx, connect.NewRequest(&v1.ListAuthEventsRequest{}))
	if err != nil {
		t.Fatalf("viewer list failed: %v", err)
	}
	if len(viewerResp.Msg.Events) != 1 || viewerResp.Msg.TotalCount != 1 {
		t.Errorf("viewer should see only 1 event, got %d (total: %d)", len(viewerResp.Msg.Events), viewerResp.Msg.TotalCount)
	}
	if viewerResp.Msg.Events[0].Username != viewer.Username {
		t.Errorf("viewer saw event for %q, expected %q", viewerResp.Msg.Events[0].Username, viewer.Username)
	}

	// Admin requests events -> sees both events
	adminCtx := WithUser(ctx, admin)
	adminResp, err := svc.ListAuthEvents(adminCtx, connect.NewRequest(&v1.ListAuthEventsRequest{}))
	if err != nil {
		t.Fatalf("admin list failed: %v", err)
	}
	if len(adminResp.Msg.Events) != 2 || adminResp.Msg.TotalCount != 2 {
		t.Errorf("admin should see 2 events, got %d (total: %d)", len(adminResp.Msg.Events), adminResp.Msg.TotalCount)
	}
}

func TestSessionManagementAndRevocation(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "sessionuser",
		PasswordHash: testDummyHash,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	otherUser, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "otheruser",
		PasswordHash: testDummyHash,
		Role:         viewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}

	now := time.Now()
	// Create 2 sessions for user
	session1, _ := queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "token-111",
		UserID:            user.ID,
		UserAgent:         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})
	session2, _ := queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "token-222",
		UserID:            user.ID,
		UserAgent:         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15",
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now.Add(-time.Hour),
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})

	// Create 1 session for other user
	sessionOther, _ := queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "token-other-333",
		UserID:            otherUser.ID,
		UserAgent:         "curl/7.88.1",
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})

	authedCtx := WithSessionID(WithUser(ctx, user), session1.SessionID)

	// 1. ListSessions returns user's 2 sessions only, with device label & is_current
	listResp, err := svc.ListSessions(authedCtx, connect.NewRequest(&v1.ListSessionsRequest{}))
	if err != nil {
		t.Fatalf("list sessions failed: %v", err)
	}
	if len(listResp.Msg.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(listResp.Msg.Sessions))
	}
	var currentFound bool
	for _, s := range listResp.Msg.Sessions {
		switch s.SessionId {
		case session1.SessionID:
			if !s.IsCurrent {
				t.Errorf("expected session1 to have IsCurrent=true")
			}
			currentFound = true
			if s.DeviceLabel != "Chrome · Windows" {
				t.Errorf("expected Chrome · Windows, got %q", s.DeviceLabel)
			}
		case session2.SessionID:
			if s.IsCurrent {
				t.Errorf("expected session2 to have IsCurrent=false")
			}
			if s.DeviceLabel != "Safari · macOS" {
				t.Errorf("expected Safari · macOS, got %q", s.DeviceLabel)
			}
		}
	}
	if !currentFound {
		t.Errorf("current session was not in list")
	}

	// 2. Revoking foreign session ID returns NotFound (no existence leak)
	_, err = svc.RevokeSession(authedCtx, connect.NewRequest(&v1.RevokeSessionRequest{
		SessionId: sessionOther.SessionID,
	}))
	if err == nil {
		t.Fatalf("expected revoking foreign session to return error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}

	// 3. Revoking own second session succeeds
	_, err = svc.RevokeSession(authedCtx, connect.NewRequest(&v1.RevokeSessionRequest{
		SessionId: session2.SessionID,
	}))
	if err != nil {
		t.Fatalf("revoke own session failed: %v", err)
	}

	// 4. RevokeAllOtherSessions preserves current session
	// Re-add a session first
	_, _ = queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         "token-444",
		UserID:            user.ID,
		UserAgent:         "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/119.0",
		ExpiresAt:         now.Add(24 * time.Hour),
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(720 * time.Hour),
	})

	revokeAllResp, err := svc.RevokeAllOtherSessions(authedCtx, connect.NewRequest(&v1.RevokeAllOtherSessionsRequest{}))
	if err != nil {
		t.Fatalf("revoke all other sessions failed: %v", err)
	}
	if revokeAllResp.Msg.RevokedCount != 1 {
		t.Errorf("expected 1 other session revoked, got %d", revokeAllResp.Msg.RevokedCount)
	}

	// Current session still valid
	_, err = queries.GetSession(ctx, session1.SessionID)
	if err != nil {
		t.Errorf("current session should still exist in DB: %v", err)
	}
}

func TestFormatDeviceLabel(t *testing.T) {
	tests := []struct {
		ua   string
		want string
	}{
		{
			ua:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
			want: "Chrome · Linux",
		},
		{
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0",
			want: "Firefox · Windows",
		},
		{
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
			want: "Safari · macOS",
		},
		{
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1",
			want: "Safari · iOS",
		},
		{
			ua:   "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120.0.6099.43 Mobile Safari/537.36",
			want: "Chrome · Android",
		},
		{
			ua:   "curl/7.88.1",
			want: "curl",
		},
		{
			ua:   "",
			want: "Unknown Device",
		},
	}

	for _, tc := range tests {
		got := formatDeviceLabel(tc.ua)
		if got != tc.want {
			t.Errorf("formatDeviceLabel(%q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
}

func TestPasskeyLoginEnabledStatus(t *testing.T) {
	queries := newTestDB(t)
	ctx := context.Background()

	// 1. Configured WebAuthn
	svc1 := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	resp1, err := svc1.GetServerStatus(ctx, connect.NewRequest(&v1.GetServerStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp1.Msg.PasskeyLoginEnabled {
		t.Errorf("expected PasskeyLoginEnabled to be true when configured")
	}
	if resp1.Msg.RpId != "localhost" {
		t.Errorf("expected RpId 'localhost', got %s", resp1.Msg.RpId)
	}

	// 2. Unconfigured WebAuthn
	svc2 := NewService(queries, slog.Default(), testAuthConfig(), config.WebAuthnConfig{}, false)
	resp2, err := svc2.GetServerStatus(ctx, connect.NewRequest(&v1.GetServerStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp2.Msg.PasskeyLoginEnabled {
		t.Errorf("expected PasskeyLoginEnabled to be false when unconfigured")
	}
}

func TestBeginPasskeyRegistration(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "alice",
		PasswordHash: testDummyHash,
		Role:         testViewerRole,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	authCtx := context.WithValue(ctx, userContextKey, user)

	// Begin registration
	resp, err := svc.BeginPasskeyRegistration(authCtx, connect.NewRequest(&v1.BeginPasskeyRegistrationRequest{
		Name: "My YubiKey",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Msg.OptionsJson == "" {
		t.Errorf("expected non-empty options_json")
	}

	// Verify challenge stored in DB
	var optMap map[string]interface{}
	unmarshalErr := json.Unmarshal([]byte(resp.Msg.OptionsJson), &optMap)
	if unmarshalErr != nil {
		t.Fatalf("failed to parse options_json: %v", unmarshalErr)
	}
	challengeStr, _ := optMap["challenge"].(string)
	if challengeStr == "" {
		t.Fatalf("expected challenge in options_json")
	}

	found, err := queries.GetUnconsumedWebAuthnChallenge(ctx, db.GetUnconsumedWebAuthnChallengeParams{
		Challenge: []byte(challengeStr),
		Kind:      challengeRegTest,
		ExpiresAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected unconsumed registration challenge in DB: %v", err)
	}
	if !found.UserID.Valid || found.UserID.Int64 != user.ID {
		t.Errorf("expected challenge user_id %d, got %v", user.ID, found.UserID)
	}
}

func TestPasskeyCredentialManagementAndLockoutGuardrail(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// 1. User with password
	userWithPass, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "bob",
		PasswordHash: testDummyHash,
		Role:         "viewer",
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Insert passkey for bob (AAGUID matching YubiKey 5 NFC: cb69481e-8ff7-4039-93ec-0a2729a1d67b)
	yubiAaguid, _ := hex.DecodeString("cb69481e8ff7403993ec0a2729a1d67b")
	credID := []byte("cred-123456789012")
	_, err = queries.CreateWebAuthnCredential(ctx, db.CreateWebAuthnCredentialParams{
		CredentialID:    credID,
		UserID:          userWithPass.ID,
		PublicKey:       []byte("pubkey-bytes"),
		AttestationType: "none",
		Transport:       "usb,nfc",
		Aaguid:          yubiAaguid,
		SignCount:       5,
		CloneWarning:    0,
		BackupEligible:  0,
		BackupState:     0,
		Name:            "My Backup Key",
		CreatedAt:       time.Now(),
		LastUsedAt:      sql.NullTime{},
	})
	if err != nil {
		t.Fatalf("failed to create credential: %v", err)
	}

	authCtx := context.WithValue(ctx, userContextKey, userWithPass)

	// List passkeys
	listResp, err := svc.ListPasskeys(authCtx, connect.NewRequest(&v1.ListPasskeysRequest{}))
	if err != nil {
		t.Fatalf("failed to list passkeys: %v", err)
	}
	if len(listResp.Msg.Passkeys) != 1 {
		t.Fatalf("expected 1 passkey, got %d", len(listResp.Msg.Passkeys))
	}
	pk := listResp.Msg.Passkeys[0]
	if pk.FriendlyDeviceName != "YubiKey 5 NFC" {
		t.Errorf("expected friendly name 'YubiKey 5 NFC', got %s", pk.FriendlyDeviceName)
	}
	if pk.Name != "My Backup Key" {
		t.Errorf("expected name 'My Backup Key', got %s", pk.Name)
	}

	// Rename passkey
	renameResp, err := svc.RenamePasskey(authCtx, connect.NewRequest(&v1.RenamePasskeyRequest{
		Id:   hex.EncodeToString(credID),
		Name: "Primary YubiKey",
	}))
	if err != nil {
		t.Fatalf("failed to rename passkey: %v", err)
	}
	if renameResp.Msg.Passkey.Name != "Primary YubiKey" {
		t.Errorf("expected renamed name 'Primary YubiKey', got %s", renameResp.Msg.Passkey.Name)
	}

	// Delete passkey succeeds because user has password
	_, err = svc.DeletePasskey(authCtx, connect.NewRequest(&v1.DeletePasskeyRequest{
		Id: hex.EncodeToString(credID),
	}))
	if err != nil {
		t.Fatalf("expected delete to succeed when user has password: %v", err)
	}

	// 2. Passwordless user lockout guardrail
	userNoPass, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username:     "passwordless",
		PasswordHash: "", // No password
		Role:         "viewer",
	})
	if err != nil {
		t.Fatalf("failed to create passwordless user: %v", err)
	}

	credID2 := []byte("cred-passwordless-1")
	_, err = queries.CreateWebAuthnCredential(ctx, db.CreateWebAuthnCredentialParams{
		CredentialID:    credID2,
		UserID:          userNoPass.ID,
		PublicKey:       []byte("pubkey-bytes"),
		AttestationType: "none",
		Transport:       "internal",
		Aaguid:          yubiAaguid,
		SignCount:       1,
		CloneWarning:    0,
		BackupEligible:  1,
		BackupState:     1,
		Name:            "Passkey 1",
		CreatedAt:       time.Now(),
		LastUsedAt:      sql.NullTime{},
	})
	if err != nil {
		t.Fatalf("failed to create credential: %v", err)
	}

	noPassCtx := context.WithValue(ctx, userContextKey, userNoPass)

	// Attempting to delete the ONLY passkey of a passwordless user MUST fail with FailedPrecondition
	_, err = svc.DeletePasskey(noPassCtx, connect.NewRequest(&v1.DeletePasskeyRequest{
		Id: hex.EncodeToString(credID2),
	}))
	if err == nil {
		t.Fatalf("expected lockout guardrail error deleting last passkey of passwordless user")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeFailedPrecondition {
		t.Errorf("expected CodeFailedPrecondition, got %v", err)
	}
}

func TestBeginPasskeyLoginAndChallengeCreation(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	resp, err := svc.BeginPasskeyLogin(ctx, connect.NewRequest(&v1.BeginPasskeyLoginRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Msg.OptionsJson == "" {
		t.Errorf("expected non-empty options_json")
	}

	// Verify challenge stored in DB
	var optMap map[string]interface{}
	unmarshalErr := json.Unmarshal([]byte(resp.Msg.OptionsJson), &optMap)
	if unmarshalErr != nil {
		t.Fatalf("failed to parse options_json: %v", unmarshalErr)
	}
	challengeStr, _ := optMap["challenge"].(string)
	if challengeStr == "" {
		t.Fatalf("expected challenge in options_json")
	}

	found, err := queries.GetUnconsumedWebAuthnChallenge(ctx, db.GetUnconsumedWebAuthnChallengeParams{
		Challenge: []byte(challengeStr),
		Kind:      challengeLoginTest,
		ExpiresAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("expected unconsumed login challenge in DB: %v", err)
	}
	if found.Consumed != 0 {
		t.Errorf("expected consumed 0, got %d", found.Consumed)
	}
}

func TestFinishPasskeyLoginInvalidPayloadAndMissingChallenge(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), testWebAuthnConfig(), false)
	ctx := context.Background()

	// 1. Malformed payload
	_, err := svc.FinishPasskeyLogin(ctx, connect.NewRequest(&v1.FinishPasskeyLoginRequest{
		ResponseJson: "invalid-json",
	}))
	if err == nil {
		t.Fatalf("expected error on malformed payload")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", err)
	}

	// 2. Valid WebAuthn structure but challenge not in DB
	authDataB64 := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEB" // valid base64
	clientDataB64 := "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0IiwiY2hhbGxlbmdlIjoibm9uZXhpc3RlbnQtY2hhbGxlbmdlLTEyMyIsIm9yaWdpbiI6Imh0dHA6Ly9sb2NhbGhvc3Q6OTI4MyJ9"

	missingChallengePayload := fmt.Sprintf(`{"id":"AQID","rawId":"AQID","type":"public-key","response":{"clientDataJSON":"%s","authenticatorData":"%s","signature":"AQID","userHandle":"AQID"}}`, clientDataB64, authDataB64)
	_, err = svc.FinishPasskeyLogin(ctx, connect.NewRequest(&v1.FinishPasskeyLoginRequest{
		ResponseJson: missingChallengePayload,
	}))
	if err == nil {
		t.Fatalf("expected error on missing challenge")
	}
	if !errors.As(err, &connectErr) || (connectErr.Code() != connect.CodeUnauthenticated && connectErr.Code() != connect.CodeInvalidArgument) {
		t.Errorf("expected CodeUnauthenticated or CodeInvalidArgument, got %v", err)
	}
}
