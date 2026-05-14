package subscription

import (
	"context"
	"time"
)

type CreateUserRequest struct {
	Name       string
	TelegramID int64
}

type CreatedUser struct {
	UUID            string
	SubscriptionURL string
	ExpiresAt       time.Time
}

type PanelUser struct {
	UUID       string
	Name       string
	TelegramID *int64
}

type Repository interface {
	GetUserByUUID(ctx context.Context, uuid string) (*Status, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*Status, string, error)
	ListPanelUsers(ctx context.Context) ([]PanelUser, error)
	SetTelegramID(ctx context.Context, uuid string, telegramID int64) error
	CreateUser(ctx context.Context, req CreateUserRequest) (*CreatedUser, error)
}
