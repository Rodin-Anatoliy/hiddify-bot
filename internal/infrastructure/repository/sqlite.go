package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error { return db.conn.Close() }

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			telegram_id   INTEGER PRIMARY KEY,
			hiddify_uuid  TEXT NOT NULL DEFAULT '',
			username      TEXT NOT NULL DEFAULT '',
			can_message   INTEGER NOT NULL DEFAULT 0,
			link_source   TEXT NOT NULL DEFAULT '',
			linked_at     DATETIME,
			last_seen     DATETIME,
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS ticket_messages (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id  INTEGER NOT NULL,
			direction    TEXT NOT NULL,
			text         TEXT NOT NULL DEFAULT '',
			file_id      TEXT NOT NULL DEFAULT '',
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_ticket_telegram_id ON ticket_messages(telegram_id);
		CREATE INDEX IF NOT EXISTS idx_users_hiddify_uuid ON users(hiddify_uuid);
	`)
	if err != nil {
		return err
	}

	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN can_message INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN link_source TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_seen DATETIME`,
	} {
		if _, alterErr := db.conn.Exec(stmt); alterErr != nil && !isDuplicateColumnErr(alterErr) {
			return alterErr
		}
	}

	return nil
}

func isDuplicateColumnErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

type UserRepository struct{ db *DB }

func NewUserRepository(db *DB) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) Save(ctx context.Context, u *user.User) error {
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
	`, u.TelegramID, u.HiddifyUUID, u.Username, boolToInt(u.CanMessage), u.LinkSource, u.LinkedAt, u.LastSeen, u.CreatedAt)
	if err != nil {
		return fmt.Errorf("user save: %w", err)
	}
	return nil
}

func (r *UserRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*user.User, error) {
	row := r.db.conn.QueryRowContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at FROM users WHERE telegram_id = ?`,
		telegramID)
	return scanUser(row)
}

func (r *UserRepository) FindByHiddifyUUID(ctx context.Context, uuid string) (*user.User, error) {
	row := r.db.conn.QueryRowContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at FROM users WHERE hiddify_uuid = ?`,
		uuid)
	return scanUser(row)
}

func (r *UserRepository) FindAllLinked(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.conn.QueryContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at FROM users WHERE hiddify_uuid != '' AND can_message = 1`)
	if err != nil {
		return nil, fmt.Errorf("user list: %w", err)
	}
	defer rows.Close()

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

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*user.User, error) {
	var u user.User
	var linkedAt sql.NullTime
	var lastSeen sql.NullTime
	var canMessage int
	err := s.Scan(&u.TelegramID, &u.HiddifyUUID, &u.Username, &canMessage, &u.LinkSource, &linkedAt, &lastSeen, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user scan: %w", err)
	}
	if linkedAt.Valid {
		t := linkedAt.Time
		u.LinkedAt = &t
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		u.LastSeen = &t
	}
	u.CanMessage = canMessage == 1
	return &u, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type TicketRepository struct{ db *DB }

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

var _ user.Repository = (*UserRepository)(nil)
var _ ticket.Repository = (*TicketRepository)(nil)

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
