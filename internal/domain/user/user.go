package user

import (
	"context"
	"time"
)

type User struct {
	TelegramID  int64
	HiddifyUUID string
	Username    string
	CanMessage  bool
	LinkSource  string
	LinkedAt    *time.Time
	LastSeen    *time.Time
	CreatedAt   time.Time
}

func (u *User) IsLinked() bool {
	return u.HiddifyUUID != ""
}

type Repository interface {
	Save(ctx context.Context, u *User) error
	SetCanMessage(ctx context.Context, telegramID int64, canMessage bool) error
	FindByTelegramID(ctx context.Context, telegramID int64) (*User, error)
	FindByHiddifyUUID(ctx context.Context, uuid string) (*User, error)
	FindAllLinked(ctx context.Context) ([]*User, error)

	// FindAllWithUUID returns all users with a HiddifyUUID regardless of CanMessage.
	FindAllWithUUID(ctx context.Context) ([]*User, error)
}
