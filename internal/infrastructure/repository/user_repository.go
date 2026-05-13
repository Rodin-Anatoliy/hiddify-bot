package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/errs"
)

type UserRepository struct{ db *DB }

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

func (r *UserRepository) SetCanMessage(ctx context.Context, telegramID int64, canMessage bool) error {
	value := 0
	if canMessage {
		value = 1
	}
	_, err := r.db.conn.ExecContext(ctx,
		`UPDATE users SET can_message = ? WHERE telegram_id = ?`, value, telegramID)
	if err != nil {
		return fmt.Errorf("user set can_message: %w", err)
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

func (r *UserRepository) FindAllLinked(ctx context.Context) ([]*user.User, error) {
	rows, err := r.db.conn.QueryContext(ctx,
		`SELECT telegram_id, hiddify_uuid, username, can_message, link_source, linked_at, last_seen, created_at
   FROM users WHERE hiddify_uuid != '' AND telegram_id > 0`)
	if err != nil {
		return nil, fmt.Errorf("user find linked: %w", err)
	}
	defer rows.Close()
	return collectUsers(rows)
}

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
		return nil, errs.ErrNotFound
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

var _ user.Repository = (*UserRepository)(nil)
