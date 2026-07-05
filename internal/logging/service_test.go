package logging

import (
	"context"
	"log/slog"
	"testing"

	connect "connectrpc.com/connect"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
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
	svc := NewService(logger)

	req := connect.NewRequest(&v1.SyncLogsRequest{
		Entries: []*v1.ClientLogEntry{
			{
				Level:     levelInfo,
				Message:   "User clicked login button",
				Timestamp: "2026-07-05T17:00:00Z",
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
	if rec0.Attrs["client_timestamp"] != "2026-07-05T17:00:00Z" {
		t.Errorf("expected client_timestamp to be 2026-07-05T17:00:00Z, got %v", rec0.Attrs["client_timestamp"])
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
