package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

func makeLinkedUsers(n int) []*user.User {
	out := make([]*user.User, n)
	now := time.Now()
	for i := range out {
		out[i] = &user.User{
			TelegramID:  int64(i + 1),
			HiddifyUUID: fmt.Sprintf("uuid-%d", i),
			CanMessage:  true,
			CreatedAt:   now,
		}
	}
	return out
}

func TestBroadcast_Recipients(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	for _, u := range makeLinkedUsers(10) {
		_ = repo.Save(ctx, u)
	}

	uc := service.NewBroadcastUseCase(repo, logger.New("debug"))

	recipients, err := uc.Recipients(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recipients) != 10 {
		t.Errorf("expected 10 recipients, got %d", len(recipients))
	}
}

func TestBroadcast_SkipsNonMessageable(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	_ = repo.Save(ctx, &user.User{
		TelegramID: 1, HiddifyUUID: "uuid-1", CanMessage: false, CreatedAt: time.Now(),
	})
	_ = repo.Save(ctx, &user.User{
		TelegramID: 2, HiddifyUUID: "uuid-2", CanMessage: true, CreatedAt: time.Now(),
	})

	uc := service.NewBroadcastUseCase(repo, logger.New("debug"))

	recipients, err := uc.Recipients(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recipients) != 1 {
		t.Errorf("expected 1 recipient, got %d", len(recipients))
	}
}
