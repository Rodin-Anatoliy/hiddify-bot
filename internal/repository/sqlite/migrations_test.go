package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedNow = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

// legacyBaseSchema is the pre-versioning schema with users as first created
// (before the ALTER-added columns).
const legacyBaseSchema = `
	CREATE TABLE IF NOT EXISTS users (
		telegram_id   INTEGER PRIMARY KEY,
		hiddify_uuid  TEXT    NOT NULL DEFAULT '',
		username      TEXT    NOT NULL DEFAULT '',
		linked_at     DATETIME,
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ticket_messages (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id  INTEGER NOT NULL,
		direction    TEXT    NOT NULL,
		text         TEXT    NOT NULL DEFAULT '',
		file_id      TEXT    NOT NULL DEFAULT '',
		created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS admin_sessions (
		message_id    INTEGER PRIMARY KEY,
		target_tg_id  INTEGER NOT NULL,
		expires_at    DATETIME NOT NULL,
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_ticket_telegram_id  ON ticket_messages(telegram_id);
	CREATE INDEX IF NOT EXISTS idx_users_hiddify_uuid  ON users(hiddify_uuid);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON admin_sessions(expires_at);
`

// legacyStartup is what the OLD binary ran on every start (all idempotent).
func legacyStartup(t *testing.T, conn *sql.DB) {
	t.Helper()
	if _, err := conn.Exec(legacyBaseSchema); err != nil {
		t.Fatalf("legacy create: %v", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN can_message INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN link_source TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_seen   DATETIME`,
	} {
		if _, err := conn.Exec(stmt); err != nil && !isDuplicateColumn(err) {
			t.Fatalf("legacy alter %q: %v", stmt, err)
		}
	}
}

func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func count(t *testing.T, conn *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	files, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return files
}

// makeLegacyDB builds a DB as the old binary left it: no schema_version, rows in every table.
func makeLegacyDB(t *testing.T, path string) {
	t.Helper()
	conn := rawOpen(t, path)
	legacyStartup(t, conn)
	for _, q := range []string{
		`INSERT INTO users (telegram_id, hiddify_uuid, username, can_message, link_source) VALUES (1, 'u-1', 'alice', 1, 'auto'), (2, '', 'bob', 0, '')`,
		`INSERT INTO ticket_messages (telegram_id, direction, text) VALUES (1, 'in', 'hello'), (1, 'out', 'hi')`,
		`INSERT INTO admin_sessions (message_id, target_tg_id, expires_at) VALUES (10, 1, '2030-01-01 00:00:00')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}
}

func TestMigrate_EmptyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if v := count(t, db.conn, `SELECT MAX(version) FROM schema_version`); v != len(migrations) {
		t.Fatalf("version = %d, want %d", v, len(migrations))
	}
	for _, tbl := range []string{"users", "ticket_messages", "admin_sessions", "invites"} {
		if n := count(t, db.conn, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='`+tbl+`'`); n != 1 {
			t.Fatalf("table %s missing", tbl)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if b := backups(t, path); len(b) != 0 {
		t.Fatalf("unexpected backups for fresh DB: %v", b)
	}
}

func TestMigrate_LegacyDBAdoptsVersioningWithBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	makeLegacyDB(t, path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if v := count(t, db.conn, `SELECT MAX(version) FROM schema_version`); v != len(migrations) {
		t.Fatalf("version = %d, want %d", v, len(migrations))
	}
	want := map[string]int{"users": 2, "ticket_messages": 2, "admin_sessions": 1}
	for tbl, n := range want {
		if got := count(t, db.conn, `SELECT COUNT(*) FROM `+tbl); got != n {
			t.Fatalf("%s rows = %d, want %d", tbl, got, n)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	b := backups(t, path)
	if len(b) != 1 || !strings.Contains(b[0], ".bak-v0-") {
		t.Fatalf("backups = %v, want one .bak-v0-*", b)
	}
	bk := rawOpen(t, b[0])
	for tbl, n := range want {
		if got := count(t, bk, `SELECT COUNT(*) FROM `+tbl); got != n {
			t.Fatalf("backup %s rows = %d, want %d", tbl, got, n)
		}
	}
	if n := count(t, bk, `SELECT COUNT(*) FROM sqlite_master WHERE name='schema_version'`); n != 0 {
		t.Fatalf("backup must be the pre-migration state")
	}
}

func TestMigrate_ReopenMakesNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	makeLegacyDB(t, path)
	for i := 0; i < 3; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		if v := count(t, db.conn, `SELECT MAX(version) FROM schema_version`); v != len(migrations) {
			t.Fatalf("version = %d, want %d", v, len(migrations))
		}
		if n := count(t, db.conn, `SELECT COUNT(*) FROM schema_version`); n != len(migrations) {
			t.Fatalf("schema_version rows = %d, want %d", n, len(migrations))
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	if b := backups(t, path); len(b) != 1 {
		t.Fatalf("backups = %v, want exactly 1", b)
	}
}

func TestMigrate_AddsMissingColumnsToOldUsersTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	conn := rawOpen(t, path)
	if _, err := conn.Exec(legacyBaseSchema); err != nil { // users without can_message/link_source/last_seen
		t.Fatalf("create: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO users (telegram_id, hiddify_uuid) VALUES (5, 'u-5')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if n := count(t, db.conn, `SELECT COUNT(*) FROM users WHERE telegram_id = 5 AND can_message = 0 AND link_source = '' AND last_seen IS NULL`); n != 1 {
		t.Fatalf("row not intact after ALTERs")
	}
}

func TestMigrate_FailingMigrationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := open(path, migrations, fixedNow)
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO users (telegram_id, hiddify_uuid) VALUES (7, 'u-7')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	boom := errors.New("boom")
	bad := append(append([]migration{}, migrations...), migration{version: len(migrations) + 1, apply: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE half_done (id INTEGER)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO users (telegram_id) VALUES (8)`); err != nil {
			return err
		}
		return boom
	}})
	if _, err := open(path, bad, fixedNow); !errors.Is(err, boom) {
		t.Fatalf("open with failing migration: err = %v, want boom", err)
	}

	conn := rawOpen(t, path)
	if v := count(t, conn, `SELECT MAX(version) FROM schema_version`); v != len(migrations) {
		t.Fatalf("version = %d, want %d", v, len(migrations))
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM sqlite_master WHERE name='half_done'`); n != 0 {
		t.Fatalf("half_done table survived rollback")
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM users`); n != 1 {
		t.Fatalf("users rows = %d, want 1", n)
	}
	b := backups(t, path)
	if want := fmt.Sprintf(".bak-v%d-", len(migrations)); len(b) != 1 || !strings.Contains(b[0], want) {
		t.Fatalf("backups = %v, want one *%s*", b, want)
	}
	if n := count(t, rawOpen(t, b[0]), `SELECT COUNT(*) FROM users`); n != 1 {
		t.Fatalf("backup users rows = %d, want 1", n)
	}
}

func TestMigrate_BackupFailureAppliesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	makeLegacyDB(t, path)

	// Put a directory on the backup target so VACUUM INTO fails on every OS
	// (an existing plain file is overwritten on Linux, so it is not a reliable blocker).
	target := path + ".bak-v0-" + fixedNow().Format("20060102T150405.000Z")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("precreate target: %v", err)
	}

	if _, err := open(path, migrations, fixedNow); err == nil {
		t.Fatal("expected error when backup fails")
	}
	conn := rawOpen(t, path)
	if n := count(t, conn, `SELECT COUNT(*) FROM sqlite_master WHERE name='schema_version'`); n != 0 {
		t.Fatal("migration was applied despite backup failure")
	}
	if n := count(t, conn, `SELECT COUNT(*) FROM users`); n != 2 {
		t.Fatalf("users rows = %d, want 2", n)
	}
}

// Forward compatibility: the OLD binary's startup statements must still succeed
// on a DB already migrated (schema_version present, extra objects added later).
func TestMigrate_OldStatementsRunOnMigratedDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	makeLegacyDB(t, path)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.conn.Exec(`CREATE TABLE invites_future (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("future table: %v", err)
	}
	if _, err := db.conn.Exec(`ALTER TABLE users ADD COLUMN future_col TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("future column: %v", err)
	}
	legacyStartup(t, db.conn)
	if n := count(t, db.conn, `SELECT COUNT(*) FROM users`); n != 2 {
		t.Fatalf("users rows = %d, want 2", n)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestMigrate_PrunesOldBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.db")
	makeLegacyDB(t, path)

	old := []string{
		"bot.db.bak-v0-20260101T000000.000Z",
		"bot.db.bak-v0-20260102T000000.000Z",
		"bot.db.bak-v10-20260103T000000.000Z",
		"bot.db.bak-v9-20260104T000000.000Z",
	}
	unrelated := []string{"other.db.bak-v0-20260101T000000.000Z", "bot.db.bak", "notes.txt"}
	for _, n := range append(append([]string{}, old...), unrelated...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}

	db, err := open(path, migrations, fixedNow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := backups(t, path)
	if len(got) != 1 || filepath.Base(got[0]) != "bot.db.bak-v0-20261001T120000.000Z" {
		t.Fatalf("backups = %v, want only the new one", got)
	}
	for _, n := range unrelated {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("unrelated file %s touched: %v", n, err)
		}
	}
}
