package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/port"
)

// IncomingMessage carries everything needed to handle a user → admin message.
type IncomingMessage struct {
	TelegramID int64
	Username   string
	Text       string
	FileID     string // non-empty when message contains a photo
}

// SupportUseCase manages bidirectional messaging between users and the admin.
type SupportUseCase struct {
	tickets ticket.Repository
	sender  port.Sender
	adminID int64
	log     *slog.Logger
}

func NewSupportUseCase(tickets ticket.Repository, sender port.Sender, adminID int64, log *slog.Logger) *SupportUseCase {
	return &SupportUseCase{
		tickets: tickets,
		sender:  sender,
		adminID: adminID,
		log:     log.With("usecase", "support"),
	}
}

// HandleUserMessage saves the message and forwards it to the admin.
// Returns the saved ticket; admin delivery failure is logged but not returned.
func (uc *SupportUseCase) HandleUserMessage(ctx context.Context, msg IncomingMessage) (*ticket.Message, error) {
	m := &ticket.Message{
		TelegramID: msg.TelegramID,
		Direction:  ticket.DirectionUserToAdmin,
		Text:       msg.Text,
		FileID:     msg.FileID,
		CreatedAt:  time.Now(),
	}
	if err := uc.tickets.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("support: save: %w", err)
	}

	forwardText := fmt.Sprintf("📩 *@%s* (`%d`)\n\n%s", msg.Username, msg.TelegramID, msg.Text)
	if err := uc.forward(ctx, uc.adminID, msg.FileID, forwardText); err != nil {
		uc.log.Warn("support: forward to admin failed", "err", err)
	}
	return m, nil
}

// HandleAdminReply saves the reply and delivers it to the target user.
func (uc *SupportUseCase) HandleAdminReply(ctx context.Context, targetTelegramID int64, text, fileID string) (*ticket.Message, error) {
	m := &ticket.Message{
		TelegramID: targetTelegramID,
		Direction:  ticket.DirectionAdminToUser,
		Text:       text,
		FileID:     fileID,
		CreatedAt:  time.Now(),
	}
	if err := uc.tickets.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("support: save reply: %w", err)
	}

	replyText := "📬 *Ответ поддержки:*\n\n" + text
	if err := uc.forward(ctx, targetTelegramID, fileID, replyText); err != nil {
		return nil, fmt.Errorf("support: deliver reply: %w", err)
	}
	return m, nil
}

func (uc *SupportUseCase) forward(ctx context.Context, telegramID int64, fileID, text string) error {
	if fileID != "" {
		return uc.sender.SendPhoto(ctx, telegramID, fileID, text)
	}
	return uc.sender.SendText(ctx, telegramID, text)
}

