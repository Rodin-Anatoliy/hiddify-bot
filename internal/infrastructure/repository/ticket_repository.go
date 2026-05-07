package repository

import (
	"context"
	"fmt"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
)

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

var _ ticket.Repository = (*TicketRepository)(nil)
