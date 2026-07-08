package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
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

func TestSendGotify(t *testing.T) {
	dbConn := newTestDBConn(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	dispatcher := NewDispatcher(dbConn, logger)
	ctx := context.Background()

	// 1. Test when Gotify settings are not configured (should do nothing/no panic)
	dispatcher.SendGotify(ctx, "Test Title", "Test Message", 5)

	// Set up a mock Gotify server
	receivedToken := ""
	var receivedBody map[string]interface{}
	mockGotify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		receivedToken = r.Header.Get("X-Gotify-Key")
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			_ = json.Unmarshal(bodyBytes, &receivedBody)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer mockGotify.Close()

	// 2. Configure Settings in SQLite DB
	queries := db.New(dbConn)
	err := queries.UpdateSetting(ctx, db.UpdateSettingParams{
		Key:   "gotify_url",
		Value: mockGotify.URL,
	})
	if err != nil {
		t.Fatalf("failed to save gotify_url: %v", err)
	}
	err = queries.UpdateSetting(ctx, db.UpdateSettingParams{
		Key:   "gotify_token",
		Value: "mock-token-abc",
	})
	if err != nil {
		t.Fatalf("failed to save gotify_token: %v", err)
	}

	// 3. Dispatch Gotify notification
	dispatcher.SendGotify(ctx, "Alert Title", "Alert Body Text", 7)

	if receivedToken != "mock-token-abc" {
		t.Errorf("expected received token 'mock-token-abc', got '%s'", receivedToken)
	}
	if receivedBody["title"] != "Alert Title" {
		t.Errorf("expected title 'Alert Title', got '%v'", receivedBody["title"])
	}
	if receivedBody["message"] != "Alert Body Text" {
		t.Errorf("expected message 'Alert Body Text', got '%v'", receivedBody["message"])
	}
	if val, ok := receivedBody["priority"].(float64); !ok || val != 7 {
		t.Errorf("expected priority 7, got '%v'", receivedBody["priority"])
	}
}
