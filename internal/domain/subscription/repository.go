package subscription

import (
	"context"
	"time"
)

// CreateUserRequest describes a panel user to create. The optional overrides
// replace the configured defaults when non-zero.
type CreateUserRequest struct {
	Name         string
	TelegramID   int64
	UsageLimitGB int    // 0 = config default
	PackageDays  int    // 0 = config default
	Mode         string // "" = config default
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
	ListStatusesByTelegramID(ctx context.Context, telegramID int64) ([]*Status, error)
	ListPanelUsers(ctx context.Context) ([]PanelUser, error)
	SetTelegramID(ctx context.Context, uuid string, telegramID int64) error
	CreateUser(ctx context.Context, req CreateUserRequest) (*CreatedUser, error)
}
