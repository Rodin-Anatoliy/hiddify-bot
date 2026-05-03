// Package repository provides SQLite-backed implementations of domain repositories.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"

	_ "modernc.org/sqlite" // pure-Go SQLite, no CGO
)

// DB wraps sql.DB with migration support.
type DB struct{ conn *sql.DB }

// Open creates or opens the SQLite database at path and runs migrations.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	// SQLite supports only one writer at a time; a single connection avoids locking errors.
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}
	return db, nil
}

// Close releases the underlying connection pool.
func (db *DB) Close() error { return db.conn.Close() }

// migrate runs idempotent DDL.
// New columns are added via ALTER TABLE — safe to run on existing databases.
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

		-- admin_sessions persists the reply context across bot restarts.
		-- When admin clicks "Reply", we store which user they're replying to.
		-- Without this table the mapping lives only in memory and is lost on restart.
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

	// Incremental migrations for existing databases — safe to re-run.
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN can_message INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN link_source TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_seen   DATETIME`,
	} {
		if _, err := db.conn.Exec(stmt); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("migration %q: %w", stmt, err)
		}
	}
	return nil
}

// isDuplicateColumn detects SQLite "duplicate column name" errors.
// modernc.org/sqlite does not expose typed error codes, so string matching is necessary.
func isDuplicateColumn(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

// ── UserRepository ────────────────────────────────────────────────────────────

// UserRepository implements domain/user.Repository on top of SQLite.
type UserRepository struct{ db *DB }

// NewUserRepository creates a UserRepository bound to the given DB.
func NewUserRepository(db *DB) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) Save(ctx context.Context, u *user.User) error {
	canMessage := 0
	if u.CanMessage {
		canMessage = 1
	}
	_, err := r.db.conn.ExecContext(ctx, `
		INSERT INTO users (telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(telegram_id) DO UPDATE SET
			hiddify_uuid = excluded.hiddify_uuid,
			username     = excluded.username,
			can_message  = excluded.can_message,
			link_source  = excluded.link_source,
			linked_at    = excluded.linked_at,
			last_seen    = excluded.last_seen
	`, u.TelegramID, u.HiddifyUUID, u.Username, canMessage, u.LinkSource, u.LinkedAt, u.LastSeen, u.CreatedAt)
	if err != nil {
		return fmt.Errorf("user save: %w", err)
	}
	return nil
}

func (r *UserRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*user.User, error) {
	row := r.db.conn.QueryRowContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at
		 FROM users WHERE telegram_id = ?`, telegramID)
	return scanUser(row)
}

func (r *UserRepository) FindByHiddifyUUID(ctx context.Context, uuid string) (*user.User, error) {
	row := r.db.conn.QueryRowContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at
		 FROM users WHERE hiddify_uuid = ?`, uuid)
	return scanUser(row)
}

// FindAllLinked returns users eligible for broadcast: linked + have pressed /start.
func (r *UserRepository) FindAllLinked(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.conn.QueryContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at
		 FROM users WHERE hiddify_uuid != '' AND can_message = 1`)
	if err != nil {
		return nil, fmt.Errorf("user find linked: %w", err)
	}
	defer rows.Close()
	return collectUsers(rows)
}

// FindAllWithUUID returns all users with a HiddifyUUID, regardless of CanMessage.
// Used by the admin /users command to show full picture including pending users.
func (r *UserRepository) FindAllWithUUID(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.conn.QueryContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at
		 FROM users WHERE hiddify_uuid != '' ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("user find all: %w", err)
	}
	defer rows.Close()
	return collectUsers(rows)
}

// ── TicketRepository ──────────────────────────────────────────────────────────

// TicketRepository implements domain/ticket.Repository on top of SQLite.
type TicketRepository struct{ db *DB }

// NewTicketRepository creates a TicketRepository bound to the given DB.
func NewTicketRepository(db *DB) *TicketRepository { return &TicketRepository{db: db} }

func (r *TicketRepository) Save(ctx context.Context, m *ticket.Message) error {
	_, err := r.db.conn.ExecContext(ctx, `
		INSERT INTO ticket_messages (telegram_id, direction, text, file_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, m.TelegramID, string(m.Direction), m.Text, m.FileID, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("ticket save: %w", err)
	}
	return nil
}

func (r *TicketRepository) FindByTelegramID(ctx context.Context, telegramID int64) ([]*ticket.Message, error) {
	rows, err := r.db.conn.QueryContext(ctx, `
		SELECT id, telegram_id, direction, text, file_id, created_at
		FROM ticket_messages WHERE telegram_id = ? ORDER BY created_at ASC
	`, telegramID)
	if err != nil {
		return nil, fmt.Errorf("ticket list: %w", err)
	}
	defer rows.Close()

	var msgs []*ticket.Message
	for rows.Next() {
		var m ticket.Message
		var dir string
		if err := rows.Scan(&m.ID, &m.TelegramID, &dir, &m.Text, &m.FileID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("ticket scan: %w", err)
		}
		m.Direction = ticket.Direction(dir)
		msgs = append(msgs, &m)
	}
	return msgs, rows.Err()
}

// ── AdminSessionRepository ────────────────────────────────────────────────────

// AdminSession represents a pending admin reply context.
// It maps a bot message_id (the message with the "Reply" button) to the
// target user's telegram_id. Persisted so restarts don't lose context.
type AdminSession struct {
	MessageID   int
	TargetTgID  int64
	ExpiresAt   time.Time
}

// AdminSessionRepository persists reply sessions across bot restarts.
type AdminSessionRepository struct{ db *DB }

// NewAdminSessionRepository creates an AdminSessionRepository bound to the given DB.
func NewAdminSessionRepository(db *DB) *AdminSessionRepository {
	return &AdminSessionRepository{db: db}
}

// Save stores or replaces a session for the given bot message_id.
func (r *AdminSessionRepository) Save(ctx context.Context, s AdminSession) error {
	_, err := r.db.conn.ExecContext(ctx, `
		INSERT INTO admin_sessions (message_id, target_tg_id, expires_at)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			target_tg_id = excluded.target_tg_id,
			expires_at   = excluded.expires_at
	`, s.MessageID, s.TargetTgID, s.ExpiresAt)
	if err != nil {
		return fmt.Errorf("session save: %w", err)
	}
	return nil
}

// Get returns the session for the given message_id if it exists and hasn't expired.
func (r *AdminSessionRepository) Get(ctx context.Context, messageID int) (*AdminSession, error) {
	var s AdminSession
	err := r.db.conn.QueryRowContext(ctx, `
		SELECT message_id, target_tg_id, expires_at
		FROM admin_sessions
		WHERE message_id = ? AND expires_at > CURRENT_TIMESTAMP
	`, messageID).Scan(&s.MessageID, &s.TargetTgID, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session get: %w", err)
	}
	return &s, nil
}

// Delete removes a session after it has been consumed.
func (r *AdminSessionRepository) Delete(ctx context.Context, messageID int) error {
	_, err := r.db.conn.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("session delete: %w", err)
	}
	return nil
}

// DeleteExpired removes all sessions past their expiry. Call periodically or on startup.
func (r *AdminSessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.conn.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, fmt.Errorf("session cleanup: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(s rowScanner) (*user.User, error) {
	var u user.User
	var linkedAt, lastSeen sql.NullTime
	var canMessage int

	err := s.Scan(
		&u.TelegramID, &u.HiddifyUUID, &u.Username,
		&canMessage, &u.LinkSource,
		&linkedAt, &lastSeen, &u.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user scan: %w", err)
	}
	u.CanMessage = canMessage == 1
	if linkedAt.Valid {
		t := linkedAt.Time
		u.LinkedAt = &t
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		u.LastSeen = &t
	}
	return &u, nil
}

func collectUsers(rows *sql.Rows) ([]*user.User, error) {
	var users []*user.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Compile-time interface assertions.
var (
	_ user.Repository   = (*UserRepository)(nil)
	_ ticket.Repository = (*TicketRepository)(nil)
)
