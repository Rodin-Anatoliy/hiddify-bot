package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
)

// IncomingMessage carries everything needed to handle a user → admin message.
type IncomingMessage struct {
	TelegramID   int64
	Username     string
	Text         string
	AttachmentID string
}

// SupportUseCase manages bidirectional messaging between users and the admin.
type SupportUseCase struct {
	tickets ticket.Repository
	log     *slog.Logger
}

func NewSupportUseCase(tickets ticket.Repository, log *slog.Logger) *SupportUseCase {
	return &SupportUseCase{
		tickets: tickets,
		log:     log.With("service", "support"),
	}
}

// HandleUserMessage saves the message and forwards it to the admin.
// Returns the saved ticket; admin delivery failure is logged but not returned.
func (uc *SupportUseCase) HandleUserMessage(ctx context.Context, msg IncomingMessage) (*ticket.Message, error) {
	m := &ticket.Message{
		TelegramID:   msg.TelegramID,
		Direction:    ticket.DirectionUserToAdmin,
		Text:         msg.Text,
		AttachmentID: msg.AttachmentID,
		CreatedAt:    time.Now(),
	}
	if err := uc.tickets.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("support: save: %w", err)
	}

	return m, nil
}

// HandleAdminReply saves the reply to the target user's support history.
func (uc *SupportUseCase) HandleAdminReply(ctx context.Context, targetTelegramID int64, text, attachmentID string) (*ticket.Message, error) {
	m := &ticket.Message{
		TelegramID:   targetTelegramID,
		Direction:    ticket.DirectionAdminToUser,
		Text:         text,
		AttachmentID: attachmentID,
		CreatedAt:    time.Now(),
	}
	if err := uc.tickets.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("support: save reply: %w", err)
	}

	return m, nil
}

// GetHistory returns the last support messages for a given user.
func (uc *SupportUseCase) GetHistory(ctx context.Context, telegramID int64) ([]*ticket.Message, error) {
	return uc.tickets.FindByTelegramID(ctx, telegramID)
}
