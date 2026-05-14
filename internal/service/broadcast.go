package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
)

// BroadcastUseCase selects users who have enabled messaging (/start).
type BroadcastUseCase struct {
	users user.Repository
	log   *slog.Logger
}

func NewBroadcastUseCase(users user.Repository, log *slog.Logger) *BroadcastUseCase {
	return &BroadcastUseCase{
		users: users,
		log:   log.With("service", "broadcast"),
	}
}

// Recipients returns every messageable linked user.
func (uc *BroadcastUseCase) Recipients(ctx context.Context) ([]*user.User, error) {
	recipients, err := uc.users.FindAllLinked(ctx)
	if err != nil {
		return nil, fmt.Errorf("broadcast: load recipients: %w", err)
	}
	if len(recipients) == 0 {
		uc.log.Info("broadcast: no recipients, skipping")
	}
	return recipients, nil
}
