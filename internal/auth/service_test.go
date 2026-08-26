package auth

import (
	"context"
	"database/sql"
	"errors"
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
)

const (
	testAdminUsername = "admin"
	testPassword      = "password12345"
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

func TestGetServerStatus(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
	ctx := context.Background()

	resp, err := svc.GetServerStatus(ctx, connect.NewRequest(&v1.GetServerStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
		Password: "wrongpassword",
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)

	user := db.User{
		ID:       42,
		Username: "bob",
		Role:     "user",
	}

	ctx := WithUser(context.Background(), user)
	resp, err := svc.GetMe(ctx, connect.NewRequest(&v1.GetMeRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.UserId != 42 || resp.Msg.Username != "bob" || resp.Msg.Role != "user" {
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
	svc := NewService(queries, slog.Default(), testAuthConfig(), false)
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
