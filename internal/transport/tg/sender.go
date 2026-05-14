package tg

import (
	"context"
	"strings"

	tele "gopkg.in/telebot.v3"
)

func (bot *Bot) SendText(ctx context.Context, telegramID int64, text string) error {
	_, err := bot.b.Send(chatByID(telegramID), text, tele.ModeMarkdown)
	if err != nil {
		bot.markUserUnreachable(ctx, telegramID, err)
	}
	return err
}

func (bot *Bot) SendPhoto(ctx context.Context, telegramID int64, fileID, caption string) error {
	_, err := bot.b.Send(chatByID(telegramID), &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}, tele.ModeMarkdown)
	if err != nil {
		bot.markUserUnreachable(ctx, telegramID, err)
	}
	return err
}

func (bot *Bot) markUserUnreachable(ctx context.Context, telegramID int64, sendErr error) {
	if !isUserUnreachableError(sendErr) {
		return
	}
	if err := bot.userUC.MarkCanMessage(ctx, telegramID, false); err != nil {
		bot.log.Warn("mark user unreachable failed", "telegram_id", telegramID, "err", err)
		return
	}
	bot.log.Info("user marked unreachable", "telegram_id", telegramID, "send_err", sendErr)
}

func isUserUnreachableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "bot was blocked") ||
		strings.Contains(msg, "chat not found") ||
		strings.Contains(msg, "user is deactivated")
}
