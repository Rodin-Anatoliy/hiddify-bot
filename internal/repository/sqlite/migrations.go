package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// migration is one numbered schema change. Versions start at 1 and must be
// consecutive. Never edit an applied migration — add a new one.
//
// Compatibility: older binaries run only their own idempotent
// CREATE ... IF NOT EXISTS / ALTER statements, so they keep working on a DB
// that has schema_version and later additions. Keep migrations additive.
type migration struct {
	version int
	apply   func(tx *sql.Tx) error
}

var migrations = []migration{
	{version: 1, apply: migrateV1},
	{version: 2, apply: migrateV2},
}

// migrateV2 adds invite codes. Only the SHA-256 of the code is stored.
// Times are Unix seconds. No foreign keys: the driver does not enforce them
// in this setup anyway.
func migrateV2(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS invites (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			code_hash      TEXT    NOT NULL UNIQUE,
			kind           TEXT    NOT NULL,
			days           INTEGER NOT NULL,
			created_at     INTEGER NOT NULL,
			expires_at     INTEGER NOT NULL,
			revoked_at     INTEGER,
			claimed_at     INTEGER,
			claimed_by_tg  INTEGER,
			redeemed_at    INTEGER,
			redeemed_uuid  TEXT
		);
	`)
	return err
}

// migrateV1 is the schema the bot had before versioning. It is idempotent so
// the existing production DB (tables present, no schema_version) adopts it
// without any data change.
func migrateV1(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			telegram_id   INTEGER PRIMARY KEY,
			hiddify_uuid  TEXT    NOT NULL DEFAULT '',
			username      TEXT    NOT NULL DEFAULT '',
			can_message   INTEGER NOT NULL DEFAULT 0,
			link_source   TEXT    NOT NULL DEFAULT '',
			linked_at     DATETIME,
			last_seen     DATETIME,
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
	`)
	if err != nil {
		return err
	}

	// Columns added to users after the first release; a failed ALTER only
	// fails that statement, the surrounding transaction stays usable.
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN can_message INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN link_source TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_seen   DATETIME`,
	} {
		if _, err := tx.Exec(stmt); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("statement %q: %w", stmt, err)
		}
	}
	return nil
}

// migrate applies pending migrations in order, each in its own transaction
// together with its schema_version row. If there are pending migrations and the
// DB is not empty, a VACUUM INTO backup is taken first; a backup failure aborts
// before anything is applied.
func migrate(conn *sql.DB, path string, migs []migration, now func() time.Time) error {
	current, hasTables, err := schemaState(conn)
	if err != nil {
		return err
	}

	var pending []migration
	for _, m := range migs {
		if m.version > current {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	if hasTables {
		if err := backup(conn, path, current, now()); err != nil {
			return err
		}
	}

	for _, m := range pending {
		if err := applyOne(conn, m, now()); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
	}
	return nil
}

// schemaState returns the current schema version (0 if schema_version is
// absent or empty) and whether the DB has any user tables.
func schemaState(conn *sql.DB) (version int, hasTables bool, err error) {
	var tables int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`,
	).Scan(&tables); err != nil {
		return 0, false, fmt.Errorf("count tables: %w", err)
	}

	var hasVersionTable int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`,
	).Scan(&hasVersionTable); err != nil {
		return 0, false, fmt.Errorf("look up schema_version: %w", err)
	}
	if hasVersionTable > 0 {
		if err := conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
			return 0, false, fmt.Errorf("read schema_version: %w", err)
		}
	}
	return version, tables > 0, nil
}

func backup(conn *sql.DB, path string, version int, at time.Time) error {
	target := fmt.Sprintf("%s.bak-v%d-%s", path, version, at.UTC().Format("20060102T150405.000Z"))
	// VACUUM cannot run inside a transaction and does not accept parameters.
	if _, err := conn.Exec(`VACUUM INTO '` + strings.ReplaceAll(target, `'`, `''`) + `'`); err != nil {
		return fmt.Errorf("backup to %s: %w", target, err)
	}
	pruneBackups(path, target)
	return nil
}

// pruneBackups removes every "<path>.bak-v*" file except the just-created keep
// one, so exactly one backup remains. Failures are only logged: they must never
// fail Open.
func pruneBackups(path, keep string) {
	dir, prefix := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	prefix += ".bak-v"
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("backup cleanup: read dir", "dir", dir, "err", err)
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, prefix) || name == filepath.Base(keep) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			slog.Warn("backup cleanup: remove old backup", "file", name, "err", err)
		}
	}
}

func applyOne(conn *sql.DB, m migration, at time.Time) (err error) {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER NOT NULL,
		applied_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	if err = m.apply(tx); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, m.version, at.UTC()); err != nil {
		return fmt.Errorf("record version: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}
