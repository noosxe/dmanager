package audit

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"dmanager/internal/db"
)

func newTestRecorder(t *testing.T) (*Recorder, *db.Queries, *sql.DB) {
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
	return NewRecorder(queries, slog.New(slog.NewTextHandler(testWriter{}, nil))), queries, dbConn
}

const (
	testActorAdmin = "admin"
	testActionDel  = "image.delete"

	auditTestDetailAged  = "aged"
	auditTestDetailFresh = "fresh"
)

type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRecordAndListRoundTrip(t *testing.T) {
	recorder, queries, _ := newTestRecorder(t)

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

// backdate rewrites an entry's created_at to now minus the given days,
// simulating an entry that has aged past the retention window.
func backdate(t *testing.T, dbConn *sql.DB, days int) {
	t.Helper()
	if _, err := dbConn.Exec(
		`UPDATE audit_logs SET created_at = datetime('now', ?) WHERE id = 1`,
		"-" + strconv.Itoa(days) + " days",
	); err != nil {
		t.Fatalf("failed to backdate entry: %v", err)
	}
}

func setRetention(t *testing.T, queries *db.Queries, value string) {
	t.Helper()
	if err := queries.UpdateSetting(context.Background(), db.UpdateSettingParams{
		Key: RetentionSettingKey, Value: value,
	}); err != nil {
		t.Fatalf("failed to set retention: %v", err)
	}
}

func TestRecordTrimsOlderThanWindow(t *testing.T) {
	recorder, queries, dbConn := newTestRecorder(t)

	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: "old"})
	setRetention(t, queries, "7")
	backdate(t, dbConn, 10) // older than the 7-day window

	// A fresh insert trims the aged-out entry on the next record.
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: "new"})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 1 || rows[0].Detail != "new" {
		t.Errorf("expected only the fresh entry after age trim, got %+v", rows)
	}
}

func TestRetentionDefaultsWhenUnset(t *testing.T) {
	recorder, queries, dbConn := newTestRecorder(t)

	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailAged})
	// No setting row exists: the default window (90 days) applies. An entry
	// aged 100 days must be trimmed by the next insert — proving the default
	// is 90, not 365.
	backdate(t, dbConn, 100)
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailFresh})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 1 || rows[0].Detail != "fresh" {
		t.Errorf("expected default 90-day window applied, got %+v", rows)
	}
}

func TestRetentionFallsBackOnInvalidValue(t *testing.T) {
	recorder, queries, dbConn := newTestRecorder(t)

	// A corrupt setting must fall back to the default window, never disable
	// trimming.
	setRetention(t, queries, "bogus")
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailAged})
	backdate(t, dbConn, 100)
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailFresh})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 1 || rows[0].Detail != "fresh" {
		t.Errorf("expected invalid setting to fall back to the default window, got %+v", rows)
	}
}

func TestRetentionHonorsConfiguredWindow(t *testing.T) {
	recorder, queries, dbConn := newTestRecorder(t)

	setRetention(t, queries, "365")
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailAged})
	backdate(t, dbConn, 100) // 100 days old — well inside a 1-year window
	recorder.Record(context.Background(), Entry{Actor: testActorAdmin, Source: SourceUser, Action: testActionDel, Detail: auditTestDetailFresh})

	rows, err := queries.ListAuditLogs(context.Background(), db.ListAuditLogsParams{Column1: "", Limit: 10})
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected both entries kept inside the 365-day window, got %d", len(rows))
	}
}

func TestRecordTruncatesDetail(t *testing.T) {
	recorder, queries, _ := newTestRecorder(t)

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
	recorder, _, dbConn := newTestRecorder(t)
	// Close the underlying storage: Record must warn-and-continue, not panic.
	if err := dbConn.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}
	recorder.Record(context.Background(), Entry{Action: testActionDel}) // must not panic
}
