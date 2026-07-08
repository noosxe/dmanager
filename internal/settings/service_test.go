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
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const adminVal = "admin"

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
	svc := NewService(dbConn, slog.Default())

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
	svc := NewService(dbConn, slog.Default())
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
	svc := NewService(dbConn, slog.Default())
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
