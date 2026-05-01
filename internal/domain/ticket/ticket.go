package ticket

import (
	"context"
	"time"
)

type Direction string

const (
	DirectionUserToAdmin Direction = "user_to_admin"
	DirectionAdminToUser Direction = "admin_to_user"
)

type Message struct {
	ID         int64
	TelegramID int64
	Direction  Direction
	Text       string
	FileID     string
	CreatedAt  time.Time
}

type Repository interface {
	Save(ctx context.Context, m *Message) error
	FindByTelegramID(ctx context.Context, telegramID int64) ([]*Message, error)
}
