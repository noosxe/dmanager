package audit

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
)

func newTestRecorder(t *testing.T, retention int) (*Recorder, *db.Queries, *sql.DB) {
	t.Helper()
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	if err := db.RunMigrations(dbConn); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	queries := db.New(dbConn)
	return NewRecorder(queries, slog.New(slog.NewTextHandler(testWriter{}, nil)), retention), queries, dbConn
}

const (
	testActorAdmin = "admin"
	testActionDel  = "image.delete"
)

type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRecordAndListRoundTrip(t *testing.T) {
	recorder, queries, _ := newTestRecorder(t, RetentionRows)

	recorder.Record(context.Background(), Entry{
		Actor:        testActorAdmin,
		ActorRole:    testActorAdmin,
		Source:       SourceUser,
		Action:       testActionDel,
		ResourceType: "image",
		ResourceID:   "sha256:abc",
		Outcome:      OutcomeSuccess,
		Detail:       "image deleted",
	})
	recorder.Record(context.Background(), Entry{
		Actor:        SystemActor,
		Source:       SourceSystem,
		Action:       "container.upgrade",
		ResourceType: "container",
		ResourceID:   "c123",
		Outcome:      OutcomeFailure,
		Detail:       "pull failed",
	})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{
		Column1: "", Limit: 50,
	})
	if err != nil {
		t.Fatalf("failed to list audit logs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(rows))
	}

	// Newest first (id DESC).
	if rows[0].Action != "container.upgrade" {
		t.Errorf("expected newest entry first, got action %q", rows[0].Action)
	}
	if rows[1].Actor != "admin" || rows[1].Outcome != OutcomeSuccess {
		t.Errorf("unexpected first-inserted entry: %+v", rows[1])
	}
}

func TestRecordTrimsToRetention(t *testing.T) {
	recorder, queries, _ := newTestRecorder(t, 3)

	for i := 0; i < 5; i++ {
		recorder.Record(context.Background(), Entry{
			Actor:  "admin",
			Source: SourceUser,
			Action: "image.delete",
			Detail: strings.Repeat("x", i),
		})
	}

	count, err := queries.CountAuditLogs(context.Background(), db.CountAuditLogsParams{})
	if err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected retention cap of 3, got %d entries", count)
	}

	// The kept rows must be the newest (highest ids).
	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 3 || rows[len(rows)-1].Detail != strings.Repeat("x", 2) {
		t.Errorf("expected the three newest entries retained, got %+v", rows)
	}
}

func TestRecordTruncatesDetail(t *testing.T) {
	recorder, queries, _ := newTestRecorder(t, RetentionRows)

	huge := strings.Repeat("e", maxDetailLen+500)
	recorder.Record(context.Background(), Entry{Action: "settings.update", Detail: huge})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 1})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Detail) != maxDetailLen {
		t.Errorf("expected detail truncated to %d chars, got %d", maxDetailLen, len(rows[0].Detail))
	}
}

func TestRecordSurvivesBrokenStorage(t *testing.T) {
	recorder, _, dbConn := newTestRecorder(t, RetentionRows)
	// Close the underlying storage: Record must warn-and-continue, not panic.
	if err := dbConn.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}
	recorder.Record(context.Background(), Entry{Action: testActionDel}) // must not panic
}

func TestNewRecorderDefaultsRetention(t *testing.T) {
	recorder := NewRecorder(nil, slog.Default(), 0)
	if recorder.retention != RetentionRows {
		t.Errorf("expected default retention %d, got %d", RetentionRows, recorder.retention)
	}
}
