package db

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/pressly/goose/v3"
)

func TestMigrationBackfillAndRollback(t *testing.T) {
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	defer func() { _ = dbConn.Close() }()

	goose.SetBaseFS(embedMigrations)
	err = goose.SetDialect("sqlite3")
	if err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}

	// 1. Run migrations up to version 2 (simulating pre-upgrade database)
	err = goose.UpTo(dbConn, "migrations", 2)
	if err != nil {
		t.Fatalf("failed to run migrations up to version 2: %v", err)
	}

	// Insert pre-upgrade user and session (v1 schema: session_id, user_id, expires_at, created_at)
	legacyExpiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	_, err = dbConn.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (1, 'legacy_admin', 'hash', 'admin')`)
	if err != nil {
		t.Fatalf("failed to insert legacy user: %v", err)
	}
	_, err = dbConn.Exec(`INSERT INTO sessions (session_id, user_id, expires_at) VALUES ('legacy-sess-1', 1, ?)`, legacyExpiry)
	if err != nil {
		t.Fatalf("failed to insert legacy session: %v", err)
	}

	// 2. Run migration 3 (00003_session_clocks.sql)
	err = goose.Up(dbConn, "migrations")
	if err != nil {
		t.Fatalf("failed to run migration 3: %v", err)
	}

	// 3. Verify backfill on legacy session
	var absoluteExpiresAt time.Time
	var lastSeenAt time.Time
	var expiresAt time.Time
	err = dbConn.QueryRow(`SELECT expires_at, last_seen_at, absolute_expires_at FROM sessions WHERE session_id = 'legacy-sess-1'`).Scan(&expiresAt, &lastSeenAt, &absoluteExpiresAt)
	if err != nil {
		t.Fatalf("failed to query backfilled session: %v", err)
	}

	if !absoluteExpiresAt.Equal(legacyExpiry) {
		t.Errorf("expected absolute_expires_at backfilled to %v, got %v", legacyExpiry, absoluteExpiresAt)
	}
	if lastSeenAt.IsZero() {
		t.Errorf("expected last_seen_at to have default value, got %v", lastSeenAt)
	}

	// 4. Test rollback (Down to version 2)
	err = goose.Down(dbConn, "migrations")
	if err != nil {
		t.Fatalf("failed to rollback migration 3: %v", err)
	}

	// Verify columns were dropped
	var colCount int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name IN ('last_seen_at', 'absolute_expires_at')`).Scan(&colCount)
	if err != nil {
		t.Fatalf("failed to check pragma_table_info: %v", err)
	}
	if colCount != 0 {
		t.Errorf("expected 0 new columns after rollback, found %d", colCount)
	}

	// 5. Migrate up again
	err = goose.Up(dbConn, "migrations")
	if err != nil {
		t.Fatalf("failed to re-run migrations up: %v", err)
	}
}
