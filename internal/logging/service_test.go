package logging

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	testTimeStr = "2026-07-05T17:00:00Z"
	testWarnMsg = "Another Warning"
)

type logRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type recordHandler struct {
	parent  *recordHandler
	attrs   []slog.Attr
	records *[]logRecord
}

func (h *recordHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]any),
	}
	// Trace back through the parent attributes
	curr := h
	for curr != nil {
		for _, a := range curr.attrs {
			rec.Attrs[a.Key] = a.Value.Any()
		}
		curr = curr.parent
	}
	// Add record's attributes
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.Any()
		return true
	})
	*h.records = append(*h.records, rec)
	return nil
}

func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recordHandler{
		parent:  h,
		attrs:   attrs,
		records: h.records,
	}
}

func (h *recordHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestSyncLogs(t *testing.T) {
	records := &[]logRecord{}
	handler := &recordHandler{
		records: records,
	}
	logger := slog.New(handler).With("module", "logging")
	svc := NewService(logger, NewRingBuffer(10))

	req := connect.NewRequest(&v1.SyncLogsRequest{
		Entries: []*v1.ClientLogEntry{
			{
				Level:     levelInfo,
				Message:   "User clicked login button",
				Timestamp: testTimeStr,
				Component: "LoginButton",
				Metadata:  `{"userID": "123"}`,
			},
			{
				Level:     "error",
				Message:   "Failed to load user settings",
				Timestamp: "2026-07-05T17:01:00Z",
				Component: "Dashboard",
			},
			{
				Level:     "DEBUG",
				Message:   "Fetched settings successfully",
				Timestamp: "2026-07-05T17:02:00Z",
			},
			{
				Level:     "UNKNOWN",
				Message:   "Fallback level test",
				Timestamp: "2026-07-05T17:03:00Z",
			},
		},
	})

	resp, err := svc.SyncLogs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Msg.ProcessedCount != 4 {
		t.Errorf("expected processed_count to be 4, got %d", resp.Msg.ProcessedCount)
	}

	if len(*records) != 4 {
		t.Fatalf("expected 4 logged records, got %d", len(*records))
	}

	// 1. Check entry 0
	rec0 := (*records)[0]
	if rec0.Level != slog.LevelInfo {
		t.Errorf("expected LevelInfo, got %v", rec0.Level)
	}
	if rec0.Message != "User clicked login button" {
		t.Errorf("expected message, got %q", rec0.Message)
	}
	if rec0.Attrs["module"] != "logging" {
		t.Errorf("expected module attribute, got %v", rec0.Attrs["module"])
	}
	if rec0.Attrs["source"] != "frontend" {
		t.Errorf("expected source to be frontend, got %v", rec0.Attrs["source"])
	}
	if rec0.Attrs["client_level"] != levelInfo {
		t.Errorf("expected client_level to be %s, got %v", levelInfo, rec0.Attrs["client_level"])
	}
	if rec0.Attrs["client_timestamp"] != testTimeStr {
		t.Errorf("expected client_timestamp to be %s, got %v", testTimeStr, rec0.Attrs["client_timestamp"])
	}
	if rec0.Attrs["component"] != "LoginButton" {
		t.Errorf("expected component to be LoginButton, got %v", rec0.Attrs["component"])
	}
	if rec0.Attrs["metadata"] != `{"userID": "123"}` {
		t.Errorf("expected metadata, got %v", rec0.Attrs["metadata"])
	}

	// 2. Check entry 1 (case insensitivity check & optional metadata exclusion)
	rec1 := (*records)[1]
	if rec1.Level != slog.LevelError {
		t.Errorf("expected LevelError for 'error', got %v", rec1.Level)
	}
	if _, ok := rec1.Attrs["metadata"]; ok {
		t.Errorf("expected metadata attribute to be omitted, but was present")
	}

	// 3. Check entry 2 (optional component exclusion)
	rec2 := (*records)[2]
	if rec2.Level != slog.LevelDebug {
		t.Errorf("expected LevelDebug, got %v", rec2.Level)
	}
	if _, ok := rec2.Attrs["component"]; ok {
		t.Errorf("expected component attribute to be omitted, but was present")
	}

	// 4. Check entry 3 (default/fallback level mapping)
	rec3 := (*records)[3]
	if rec3.Level != slog.LevelInfo {
		t.Errorf("expected LevelInfo for unknown level 'UNKNOWN', got %v", rec3.Level)
	}
}

func TestGetSystemLogs(t *testing.T) {
	buf := NewRingBuffer(5)
	svc := NewService(slog.Default(), buf)

	buf.Add(&v1.LogEntry{Level: "INFO", Message: "Message 1", Timestamp: testTimeStr, Component: "CompA"})
	buf.Add(&v1.LogEntry{Level: levelError, Message: "Message 2", Timestamp: "2026-07-05T17:01:00Z", Component: "CompB"})
	buf.Add(&v1.LogEntry{Level: "WARN", Message: testWarnMsg, Timestamp: "2026-07-05T17:02:00Z", Component: "CompA"})

	// Test 1: Get all (should return newest first)
	req := connect.NewRequest(&v1.GetSystemLogsRequest{Limit: 10})
	resp, err := svc.GetSystemLogs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Msg.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(resp.Msg.Entries))
	}
	if resp.Msg.Entries[0].Message != testWarnMsg {
		t.Errorf("expected newest first ('%s'), got %q", testWarnMsg, resp.Msg.Entries[0].Message)
	}

	// Test 2: Filter by level
	reqFilter := connect.NewRequest(&v1.GetSystemLogsRequest{Limit: 10, LevelFilter: levelError})
	respFilter, err := svc.GetSystemLogs(context.Background(), reqFilter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(respFilter.Msg.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(respFilter.Msg.Entries))
	}
	if respFilter.Msg.Entries[0].Level != levelError {
		t.Errorf("expected %s level, got %s", levelError, respFilter.Msg.Entries[0].Level)
	}

	// Test 3: Filter by query
	reqQuery := connect.NewRequest(&v1.GetSystemLogsRequest{Limit: 10, SearchQuery: "Warning"})
	respQuery, err := svc.GetSystemLogs(context.Background(), reqQuery)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(respQuery.Msg.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(respQuery.Msg.Entries))
	}
	if respQuery.Msg.Entries[0].Message != testWarnMsg {
		t.Errorf("expected '%s', got %q", testWarnMsg, respQuery.Msg.Entries[0].Message)
	}
}

func TestInterceptHandler(t *testing.T) {
	buf := NewRingBuffer(10)
	baseLogger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil)) // discard output
	handler := NewInterceptHandler(baseLogger.Handler(), buf)
	logger := slog.New(handler)

	logger.Info("Hello System", slog.String("component", "TestComponent"), slog.String("meta_key", "meta_val"))

	entries := buf.Get(10, "", "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Message != "Hello System" {
		t.Errorf("expected message 'Hello System', got %q", entry.Message)
	}
	if entry.Component != "TestComponent" {
		t.Errorf("expected component 'TestComponent', got %q", entry.Component)
	}
	if !strings.Contains(entry.Metadata, `"meta_key":"meta_val"`) {
		t.Errorf("expected metadata to contain meta_key, got %q", entry.Metadata)
	}
}
