package usecase_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

type mockSender struct {
	textCount  atomic.Int32
	photoCount atomic.Int32
	failAfter  int32
}

func (m *mockSender) SendText(_ context.Context, _ int64, _ string) error {
	m.textCount.Add(1)
	return nil
}
func (m *mockSender) SendPhoto(_ context.Context, _ int64, _, _ string) error {
	m.photoCount.Add(1)
	return nil
}

func linkedUsers(n int) []*user.User {
	users := make([]*user.User, n)
	now := time.Now()
	for i := range users {
		users[i] = &user.User{
			TelegramID:  int64(i + 1),
			HiddifyUUID: "uuid-" + string(rune('a'+i)),
			CanMessage:  true,
			CreatedAt:   now,
		}
	}
	return users
}

func TestBroadcast_TextToAllLinked(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	for _, u := range linkedUsers(10) {
		_ = repo.Save(ctx, u)
	}

	sender := &mockSender{}
	log := logger.New("debug")
	uc := usecase.NewBroadcastUseCase(repo, sender, log)

	result, err := uc.Send(ctx, usecase.BroadcastMessage{Text: "Hello!"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 10 {
		t.Errorf("expected total=10, got %d", result.Total)
	}
	if result.Success != 10 {
		t.Errorf("expected success=10, got %d", result.Success)
	}
	if sender.textCount.Load() != 10 {
		t.Errorf("expected 10 text sends, got %d", sender.textCount.Load())
	}
}

func TestBroadcast_PhotoMessage(t *testing.T) {
	repo := newMockUserRepo()
	ctx := context.Background()
	for _, u := range linkedUsers(3) {
		_ = repo.Save(ctx, u)
	}

	sender := &mockSender{}
	log := logger.New("debug")
	uc := usecase.NewBroadcastUseCase(repo, sender, log)

	result, err := uc.Send(ctx, usecase.BroadcastMessage{Text: "Caption", FileID: "photo123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 3 || result.Success != 3 {
		t.Errorf("unexpected result: %+v", result)
	}
	if sender.photoCount.Load() != 3 {
		t.Errorf("expected 3 photo sends, got %d", sender.photoCount.Load())
	}
}
