package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/repository"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/errs"
)

func (bot *Bot) handleSupportPrompt(c tele.Context) error {
	return c.Send("📨 Напишите ваш вопрос следующим сообщением — ответим как можно скорее.")
}

func (bot *Bot) routeText(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		// Check if admin is in the middle of a bind wizard.
		if consumed, err := bot.tryHandleBindWizard(c); consumed {
			return err
		}
		return bot.handleAdminReply(c)
	}
	return bot.handleUserText(c)
}

func (bot *Bot) routePhoto(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		return nil
	}
	return bot.handleUserPhoto(c)
}

func (bot *Bot) handleUserText(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if _, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Text(),
	}); err != nil {
		bot.log.Error("support: handle text", "err", err)
		return c.Send("⚠️ Не удалось доставить сообщение. Попробуйте позже.")
	}

	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		fmt.Sprintf("📩 *@%s* (`%d`)\n\n%s", c.Sender().Username, c.Sender().ID, c.Text()),
		tele.ModeMarkdown,
		replyMarkup(c.Sender().ID),
	); err != nil {
		bot.log.Warn("support: notify admin failed", "err", err)
	}

	return c.Send("✅ Сообщение получено — ответим скоро!")
}

func (bot *Bot) handleUserPhoto(c tele.Context) error {
	photo := c.Message().Photo
	if photo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if _, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Message().Caption,
		FileID:     photo.FileID,
	}); err != nil {
		bot.log.Error("support: handle photo", "err", err)
		return c.Send("⚠️ Не удалось доставить фото.")
	}

	caption := fmt.Sprintf("📸 *@%s* (`%d`) прислал фото.\n\n%s",
		c.Sender().Username, c.Sender().ID, c.Message().Caption)
	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		&tele.Photo{File: tele.File{FileID: photo.FileID}, Caption: caption},
		tele.ModeMarkdown,
		replyMarkup(c.Sender().ID),
	); err != nil {
		bot.log.Warn("support: notify admin (photo) failed", "err", err)
	}

	return c.Send("✅ Фото получено!")
}

func (bot *Bot) handleReplyCallback(c tele.Context) error {
	targetID, err := strconv.ParseInt(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Неверный ID пользователя"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	expiresAt := time.Now().Add(replyTTL)
	if err := bot.sessionRepo.Save(ctx, repository.AdminSession{
		MessageID:  c.Message().ID,
		TargetTgID: targetID,
		ExpiresAt:  expiresAt,
	}); err != nil {
		bot.log.Error("session save failed", "err", err)
	}
	if err := bot.sessionRepo.Save(ctx, repository.AdminSession{
		MessageID:  activeAdminReplySessionID(bot.adminID),
		TargetTgID: targetID,
		ExpiresAt:  expiresAt,
	}); err != nil {
		bot.log.Error("active session save failed", "err", err)
	}
	bot.setActiveReplyMessageID(c.Message().ID)

	_ = c.Respond(&tele.CallbackResponse{
		Text: fmt.Sprintf("Следующее сообщение уйдёт пользователю %d", targetID),
	})
	if _, err := bot.b.EditReplyMarkup(c.Message(), activeReplyMarkup(targetID)); err != nil {
		bot.log.Warn("active reply markup failed", "err", err)
	}
	return nil
}

func (bot *Bot) handleAdminReply(c tele.Context) error {
	if strings.HasPrefix(c.Text(), "/") {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	session, sessionKey, err := bot.findAdminReplySession(ctx, c)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		bot.log.Error("session get failed", "err", err)
		return nil
	}

	if err := bot.sessionRepo.Delete(ctx, sessionKey); err != nil {
		bot.log.Warn("session delete failed", "err", err)
	}
	if sessionKey != activeAdminReplySessionID(bot.adminID) {
		if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
			bot.log.Warn("active session delete failed", "err", err)
		}
	}

	if _, err := bot.supportUC.HandleAdminReply(ctx, session.TargetTgID, c.Text(), ""); err != nil {
		bot.log.Error("support: admin reply failed", "err", err)
		return c.Send("⚠️ Не удалось отправить ответ.")
	}
	if sessionKey == activeAdminReplySessionID(bot.adminID) {
		bot.restoreActiveReplyMarkup(session.TargetTgID)
	}
	return c.Send(
		fmt.Sprintf("✅ Ответ отправлен пользователю `%d`.", session.TargetTgID),
		tele.ModeMarkdown,
	)
}

func (bot *Bot) handleCancelReply(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
		bot.log.Warn("active session delete failed", "err", err)
		return c.Send("⚠️ Не удалось отменить активный ответ.")
	}
	bot.clearActiveReplyMessageID()
	return c.Send("✅ Активный ответ отменён.")
}

func (bot *Bot) cancelAdminReply(ctx context.Context, c tele.Context) error {
	session, err := bot.sessionRepo.Get(ctx, c.Message().ID)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		bot.log.Warn("reply session get failed", "err", err)
	}
	if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
		bot.log.Warn("active session delete failed", "err", err)
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось отменить"})
	}
	bot.clearActiveReplyMessageID()
	if session != nil {
		if _, err := bot.b.EditReplyMarkup(c.Message(), replyMarkup(session.TargetTgID)); err != nil {
			bot.log.Warn("reply markup restore failed", "err", err)
		}
	}
	return c.Respond(&tele.CallbackResponse{Text: "Ответ отменён"})
}

func (bot *Bot) findAdminReplySession(ctx context.Context, c tele.Context) (*repository.AdminSession, int, error) {
	if replyTo := c.Message().ReplyTo; replyTo != nil {
		session, err := bot.sessionRepo.Get(ctx, replyTo.ID)
		if err == nil {
			return session, replyTo.ID, nil
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return nil, 0, err
		}
	}

	key := activeAdminReplySessionID(bot.adminID)
	session, err := bot.sessionRepo.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	return session, key, nil
}
