package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
)

type SupportUseCase struct {
	tickets ticket.Repository
	sender  Sender
	adminID int64
	log     *slog.Logger
}

func NewSupportUseCase(tickets ticket.Repository, sender Sender, adminID int64, log *slog.Logger) *SupportUseCase {
	return &SupportUseCase{
		tickets: tickets,
		sender:  sender,
		adminID: adminID,
		log:     log.With("usecase", "support"),
	}
}

func (uc *SupportUseCase) HandleUserMessage(ctx context.Context, telegramID int64, username, text, fileID string) (*ticket.Message, error) {
	m := &ticket.Message{
		TelegramID: telegramID,
		Direction:  ticket.DirectionUserToAdmin,
		Text:       text,
		FileID:     fileID,
		CreatedAt:  time.Now(),
	}
	if err := uc.tickets.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("support: save ticket: %w", err)
	}

	forwardText := fmt.Sprintf("📩 *Сообщение от* @%s (ID: `%d`)\n\n%s", username, telegramID, text)

	var deliveryErr error
	if fileID != "" {
		deliveryErr = uc.sender.SendPhoto(ctx, uc.adminID, fileID, forwardText)
	} else {
		deliveryErr = uc.sender.SendText(ctx, uc.adminID, forwardText)
	}
	if deliveryErr != nil {
		uc.log.Warn("support: could not forward to admin", "err", deliveryErr)
	}
	return m, nil
}

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

	var deliveryErr error
	if fileID != "" {
		deliveryErr = uc.sender.SendPhoto(ctx, targetTelegramID, fileID, "📬 *Ответ поддержки:*\n\n"+text)
	} else {
		deliveryErr = uc.sender.SendText(ctx, targetTelegramID, "📬 *Ответ поддержки:*\n\n"+text)
	}
	if deliveryErr != nil {
		return nil, fmt.Errorf("support: deliver reply: %w", deliveryErr)
	}
	return m, nil
}
