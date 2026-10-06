package db

import (
	"context"
	"database/sql"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/pressly/goose/v3"
)

// newMigrationTestDB opens an in-memory SQLite database wired for goose runs
// over the embedded migrations, the same way production RunMigrations does.
func newMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	dbConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = dbConn.Close() })

	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	return dbConn
}

// maxEmbeddedMigrationVersion returns the highest 000NN prefix found in the
// embedded migrations, so tests can assert the harness reaches the true head
// of the chain instead of a hardcoded version that silently goes stale.
func maxEmbeddedMigrationVersion(t *testing.T) int64 {
	t.Helper()
	entries, err := fs.ReadDir(embedMigrations, "migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations: %v", err)
	}
	var max int64
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || len(name) < 5 {
			continue
		}
		v, err := strconv.ParseInt(name[:5], 10, 64)
		if err != nil {
			t.Fatalf("unexpected migration filename %q: %v", name, err)
		}
		if v > max {
			max = v
		}
	}
	if max == 0 {
		t.Fatal("no migrations found in embedded FS")
	}
	return max
}

// TestMigrationsWalkToHead walks the full chain from an empty database and
// asserts the resulting schema version matches the embedded files, then
// exercises the full down-and-up cycle. Any new migration is automatically
// covered; a broken Up or Down fails here instead of at container start.
func TestMigrationsWalkToHead(t *testing.T) {
	dbConn := newMigrationTestDB(t)
	max := maxEmbeddedMigrationVersion(t)

	if err := goose.Up(dbConn, "migrations"); err != nil {
		t.Fatalf("failed to migrate to head: %v", err)
	}
	version, err := goose.GetDBVersion(dbConn)
	if err != nil {
		t.Fatalf("failed to read schema version: %v", err)
	}
	if version != max {
		t.Errorf("schema version after Up = %d, want %d (harness did not reach the head of the embedded chain)", version, max)
	}

	if err := goose.DownTo(dbConn, "migrations", 0); err != nil {
		t.Fatalf("failed to roll back to zero: %v", err)
	}
	if err := goose.Up(dbConn, "migrations"); err != nil {
		t.Fatalf("failed to re-migrate to head: %v", err)
	}
	version, err = goose.GetDBVersion(dbConn)
	if err != nil {
		t.Fatalf("failed to read schema version after re-up: %v", err)
	}
	if version != max {
		t.Errorf("schema version after down/up cycle = %d, want %d", version, max)
	}
}

// TestMigrationWebAuthnChallengeExtensions pins the 00008 contract on a
// populated schema-v7 database — the exact production shape this migration
// shipped into (schema v7 with live challenge rows). The first attempt used
// ADD COLUMN ... NOT NULL DEFAULT NULL, which SQLite rejects outright, and
// crash-looped the container at startup; this test is the regression pin for
// that failure mode (see #306 field notes).
func TestMigrationWebAuthnChallengeExtensions(t *testing.T) {
	dbConn := newMigrationTestDB(t)

	// Schema v7: pre-extensions challenge table, exactly as production ran.
	if err := goose.UpTo(dbConn, "migrations", 7); err != nil {
		t.Fatalf("failed to migrate to version 7: %v", err)
	}

	// Representative rows written by the v7-generated code path.
	if _, err := dbConn.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (1, 'webauthn_admin', 'hash', 'admin')`); err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
	legacyChallenge := []byte{0x00, 0x11}
	legacyExpiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if _, err := dbConn.Exec(
		`INSERT INTO webauthn_challenges (challenge, kind, user_id, expires_at, consumed) VALUES (?, 'registration', 1, ?, 0)`,
		legacyChallenge, legacyExpiry,
	); err != nil {
		t.Fatalf("failed to insert v7 challenge row: %v", err)
	}

	// The migration must apply cleanly on the populated database.
	if err := goose.UpTo(dbConn, "migrations", 8); err != nil {
		t.Fatalf("failed to apply migration 8 on populated v7 database: %v", err)
	}

	// Schema contract: extensions exists and is nullable. NOT NULL here is
	// the crash-loop defect: SQLite forbids ADD COLUMN with NOT NULL and a
	// NULL default, and pre-migration rows have no extension state to fill.
	var colCount int
	if err := dbConn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('webauthn_challenges') WHERE name = 'extensions'`,
	).Scan(&colCount); err != nil || colCount != 1 {
		t.Fatalf("extensions column missing after migration 8 (count=%d): %v", colCount, err)
	}
	var notnull int
	if err := dbConn.QueryRow(
		`SELECT "notnull" FROM pragma_table_info('webauthn_challenges') WHERE name = 'extensions'`,
	).Scan(&notnull); err != nil {
		t.Fatalf("failed to read extensions column metadata: %v", err)
	}
	if notnull != 0 {
		t.Errorf("webauthn_challenges.extensions must be nullable, got notnull=%d", notnull)
	}

	// Pre-existing rows survive with NULL extension state.
	var ext []byte
	if err := dbConn.QueryRow(
		`SELECT extensions FROM webauthn_challenges WHERE challenge = ?`, legacyChallenge,
	).Scan(&ext); err != nil {
		t.Fatalf("failed to read pre-migration challenge row: %v", err)
	}
	if ext != nil {
		t.Errorf("pre-migration challenge row should have NULL extensions, got %q", ext)
	}

	// Generated-code round trip: NULL scans as nil through the queries layer.
	queries := New(dbConn)
	ctx := context.Background()
	got, err := queries.GetUnconsumedWebAuthnChallenge(ctx, GetUnconsumedWebAuthnChallengeParams{
		Challenge: legacyChallenge,
		Kind:      "registration",
		ExpiresAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to fetch pre-migration challenge: %v", err)
	}
	if got.Extensions != nil {
		t.Errorf("pre-migration challenge extensions = %q, want nil", got.Extensions)
	}

	// New rows persist the marshaled SessionExtensions beside the challenge
	// (the #307 contract the column exists for).
	created, err := queries.CreateWebAuthnChallenge(ctx, CreateWebAuthnChallengeParams{
		Challenge:  []byte{0xde, 0xad},
		Kind:       "login",
		UserID:     sql.NullInt64{Int64: 1, Valid: true},
		ExpiresAt:  time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second),
		Extensions: []byte(`{"credProps":{"rk":true}}`),
	})
	if err != nil {
		t.Fatalf("failed to create challenge with extensions: %v", err)
	}
	if string(created.Extensions) != `{"credProps":{"rk":true}}` {
		t.Errorf("created challenge extensions = %q, want persisted JSON", created.Extensions)
	}
}
