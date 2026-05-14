package sqlite

import (
	"fmt"
	"strings"
)

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
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

	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN can_message INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN link_source TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_seen   DATETIME`,
	} {
		if _, err := db.conn.Exec(stmt); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("migration %q: %w", stmt, err)
		}
	}

	// temporary fix
	_, _ = db.conn.Exec(`UPDATE users SET can_message = 1 WHERE telegram_id > 0 AND hiddify_uuid != ''`)

	return nil
}

func isDuplicateColumn(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}
