package service_test

import (
	"context"
	"testing"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

type mockTicketRepo struct {
	messages []*ticket.Message
}

func (m *mockTicketRepo) Save(_ context.Context, msg *ticket.Message) error {
	cp := *msg
	m.messages = append(m.messages, &cp)
	return nil
}

func (m *mockTicketRepo) FindByTelegramID(_ context.Context, telegramID int64) ([]*ticket.Message, error) {
	var out []*ticket.Message
	for _, msg := range m.messages {
		if msg.TelegramID == telegramID {
			cp := *msg
			out = append(out, &cp)
		}
	}
	return out, nil
}

func TestSupport_HandleUserMessage_SavesIncoming(t *testing.T) {
	repo := &mockTicketRepo{}
	uc := service.NewSupportUseCase(repo, logger.New("debug"))

	msg, err := uc.HandleUserMessage(context.Background(), service.IncomingMessage{
		TelegramID: 42,
		Username:   "alice",
		Text:       "help",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Direction != ticket.DirectionUserToAdmin {
		t.Fatalf("expected user_to_admin, got %s", msg.Direction)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 saved message, got %d", len(repo.messages))
	}
}

func TestSupport_HandleAdminReply_SavesReply(t *testing.T) {
	repo := &mockTicketRepo{}
	uc := service.NewSupportUseCase(repo, logger.New("debug"))

	msg, err := uc.HandleAdminReply(context.Background(), 42, "answer", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Direction != ticket.DirectionAdminToUser {
		t.Fatalf("expected admin_to_user, got %s", msg.Direction)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("expected 1 saved reply, got %d", len(repo.messages))
	}
}
