package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
)

type AdminSession struct {
	MessageID  int
	TargetTgID int64
	ExpiresAt  time.Time
}

type AdminSessionRepository struct{ db *DB }

func NewAdminSessionRepository(db *DB) *AdminSessionRepository {
	return &AdminSessionRepository{db: db}
}

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

func (r *AdminSessionRepository) Delete(ctx context.Context, messageID int) error {
	_, err := r.db.conn.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("session delete: %w", err)
	}
	return nil
}

func (r *AdminSessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.conn.ExecContext(ctx,
		`DELETE FROM admin_sessions WHERE expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, fmt.Errorf("session cleanup: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
