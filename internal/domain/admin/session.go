package admin

import (
	"context"
	"time"
)

type Session struct {
	MessageID  int
	TargetTgID int64
	ExpiresAt  time.Time
}

type SessionRepository interface {
	Save(ctx context.Context, s Session) error
	Get(ctx context.Context, messageID int) (*Session, error)
	Delete(ctx context.Context, messageID int) error
}
