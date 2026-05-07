package usecase_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

var _ usecase.Sender = (*mockSender)(nil)

type mockSender struct {
	texts  chan struct{}
	photos chan struct{}
}

func newMockSender(buf int) *mockSender {
	return &mockSender{
		texts:  make(chan struct{}, buf),
		photos: make(chan struct{}, buf),
	}
}

func (m *mockSender) SendText(_ context.Context, _ int64, _ string) error {
	m.texts <- struct{}{}
	return nil
}

func (m *mockSender) SendPhoto(_ context.Context, _ int64, _, _ string) error {
	m.photos <- struct{}{}
	return nil
}

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

func TestBroadcast_Text(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	for _, u := range makeLinkedUsers(10) {
		_ = repo.Save(ctx, u)
	}

	sender := newMockSender(10)
	uc := usecase.NewBroadcastUseCase(repo, sender, logger.New("debug"))

	result, err := uc.Send(ctx, usecase.BroadcastMessage{Text: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 10 || result.Success != 10 {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(sender.texts) != 10 {
		t.Errorf("expected 10 sends, got %d", len(sender.texts))
	}
}

func TestBroadcast_Photo(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	for _, u := range makeLinkedUsers(3) {
		_ = repo.Save(ctx, u)
	}

	sender := newMockSender(3)
	uc := usecase.NewBroadcastUseCase(repo, sender, logger.New("debug"))

	result, err := uc.Send(ctx, usecase.BroadcastMessage{Text: "caption", FileID: "photo123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 3 || result.Success != 3 {
		t.Errorf("unexpected result: %+v", result)
	}
	if len(sender.photos) != 3 {
		t.Errorf("expected 3 photo sends, got %d", len(sender.photos))
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

	sender := newMockSender(2)
	uc := usecase.NewBroadcastUseCase(repo, sender, logger.New("debug"))

	result, err := uc.Send(ctx, usecase.BroadcastMessage{Text: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected total=1 (only messageable), got %d", result.Total)
	}
}
