package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/auth"
	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	adminVal = "admin"
	ghcrHost = "ghcr.io"
)

func newTestDBConn(t *testing.T) *sql.DB {
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return dbConn
}

func TestGetSettings(t *testing.T) {
	dbConn := newTestDBConn(t)
	svc := NewService(dbConn, slog.Default(), nil, nil)

	// Test Unauthenticated
	ctx := context.Background()
	_, err := svc.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err == nil {
		t.Fatal("expected error for unauthenticated user, got nil")
	}

	// Test Authenticated but non-admin
	user := db.User{
		Username: "user1",
		Role:     "viewer",
	}
	ctx = auth.WithUser(ctx, user)
	_, err = svc.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err == nil {
		t.Fatal("expected error for non-admin user, got nil")
	}

	// Test Admin
	adminUser := db.User{
		Username: adminVal,
		Role:     adminVal,
	}
	ctx = auth.WithUser(ctx, adminUser)
	resp, err := svc.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GotifyUrl != "" || resp.Msg.GotifyToken != "" {
		t.Errorf("expected empty settings initially, got %+v", resp.Msg)
	}
}

func TestUpdateSettings(t *testing.T) {
	dbConn := newTestDBConn(t)
	svc := NewService(dbConn, slog.Default(), nil, nil)
	adminUser := db.User{
		Username: adminVal,
		Role:     adminVal,
	}
	ctx := auth.WithUser(context.Background(), adminUser)

	// Update settings
	_, err := svc.UpdateSettings(ctx, connect.NewRequest(&v1.UpdateSettingsRequest{
		GotifyUrl:   "http://localhost:8080",
		GotifyToken: "tok123",
	}))
	if err != nil {
		t.Fatalf("unexpected error updating settings: %v", err)
	}

	// Verify update in GetSettings
	getResp, err := svc.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil {
		t.Fatalf("unexpected error getting settings: %v", err)
	}
	if getResp.Msg.GotifyUrl != "http://localhost:8080" || getResp.Msg.GotifyToken != "tok123" {
		t.Errorf("unexpected settings values: %+v", getResp.Msg)
	}
}

func TestTestGotifyNotification(t *testing.T) {
	dbConn := newTestDBConn(t)
	svc := NewService(dbConn, slog.Default(), nil, nil)
	adminUser := db.User{
		Username: adminVal,
		Role:     adminVal,
	}
	ctx := auth.WithUser(context.Background(), adminUser)

	// Set up a mock Gotify server
	receivedToken := ""
	var receivedBody map[string]interface{}
	mockGotify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/message" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		receivedToken = r.Header.Get("X-Gotify-Key")
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			_ = json.Unmarshal(bodyBytes, &receivedBody)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer mockGotify.Close()

	// 1. Test Gotify connection with custom unstaged parameters
	testResp, err := svc.TestGotifyNotification(ctx, connect.NewRequest(&v1.TestGotifyNotificationRequest{
		GotifyUrl:   mockGotify.URL,
		GotifyToken: "my-test-token",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !testResp.Msg.Success {
		t.Fatalf("expected test success, got error: %s", testResp.Msg.ErrorMessage)
	}
	if receivedToken != "my-test-token" {
		t.Errorf("expected received token 'my-test-token', got '%s'", receivedToken)
	}
	if receivedBody["title"] != "DManager Connection Test" {
		t.Errorf("unexpected body title: %v", receivedBody["title"])
	}

	// 2. Save settings and test with empty/default parameters fallback
	_, err = svc.UpdateSettings(ctx, connect.NewRequest(&v1.UpdateSettingsRequest{
		GotifyUrl:   mockGotify.URL,
		GotifyToken: "saved-token",
	}))
	if err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}

	testResp2, err := svc.TestGotifyNotification(ctx, connect.NewRequest(&v1.TestGotifyNotificationRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !testResp2.Msg.Success {
		t.Fatalf("expected test success with saved settings, got error: %s", testResp2.Msg.ErrorMessage)
	}
	if receivedToken != "saved-token" {
		t.Errorf("expected received token 'saved-token', got '%s'", receivedToken)
	}
}

func TestGetRegistryStatus(t *testing.T) {
	dbConn := newTestDBConn(t)
	adminUser := db.User{
		Username: adminVal,
		Role:     adminVal,
	}
	ctx := auth.WithUser(context.Background(), adminUser)

	// Case 1: Empty registries
	svc1 := NewService(dbConn, slog.Default(), nil, nil)
	resp1, err := svc1.GetRegistryStatus(ctx, connect.NewRequest(&v1.GetRegistryStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp1.Msg.Registries) != 0 {
		t.Errorf("expected 0 registries, got %d", len(resp1.Msg.Registries))
	}

	// Case 2: Unconfigured registry (missing credentials)
	regs2 := []config.Registry{
		{Host: ghcrHost, Username: ""}, // missing password/username
	}
	svc2 := NewService(dbConn, slog.Default(), regs2, nil)
	resp2, err := svc2.GetRegistryStatus(ctx, connect.NewRequest(&v1.GetRegistryStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp2.Msg.Registries) != 1 {
		t.Fatalf("expected 1 registry status, got %d", len(resp2.Msg.Registries))
	}
	r2 := resp2.Msg.Registries[0]
	if r2.Host != ghcrHost {
		t.Errorf("expected host ghcr.io, got %s", r2.Host)
	}
	if r2.IsConfigured {
		t.Errorf("expected IsConfigured to be false")
	}
	if r2.IsHealthy {
		t.Errorf("expected IsHealthy to be false")
	}
	if r2.ErrorMessage == "" {
		t.Errorf("expected error message")
	}

	// Case 3: Configured registry but nil docker client
	regs3 := []config.Registry{
		{Host: ghcrHost, Username: "user", Password: "pwd"},
	}
	svc3 := NewService(dbConn, slog.Default(), regs3, nil)
	resp3, err := svc3.GetRegistryStatus(ctx, connect.NewRequest(&v1.GetRegistryStatusRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp3.Msg.Registries) != 1 {
		t.Fatalf("expected 1 registry status, got %d", len(resp3.Msg.Registries))
	}
	r3 := resp3.Msg.Registries[0]
	if !r3.IsConfigured {
		t.Errorf("expected IsConfigured to be true")
	}
	if r3.IsHealthy {
		t.Errorf("expected IsHealthy to be false")
	}
	if r3.ErrorMessage != "Docker client is not initialized on host" {
		t.Errorf("unexpected error message: %s", r3.ErrorMessage)
	}
}
