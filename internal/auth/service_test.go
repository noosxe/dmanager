package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	testAdminUsername = "admin"
	testPassword      = "password123"
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

func TestGetServerStatus(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default())
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
	svc := NewService(queries, slog.Default())
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
	svc := NewService(queries, slog.Default())
	ctx := context.Background()

	_, err := svc.SetupAdmin(ctx, connect.NewRequest(&v1.SetupAdminRequest{
		Username: testAdminUsername,
		Password: testPassword,
	}))
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	resp, err := svc.Login(ctx, connect.NewRequest(&v1.LoginRequest{
		Username: testAdminUsername,
		Password: testPassword,
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

func TestGetMe(t *testing.T) {
	queries := newTestDB(t)
	svc := NewService(queries, slog.Default())

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
	svc := NewService(queries, slog.Default())
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
