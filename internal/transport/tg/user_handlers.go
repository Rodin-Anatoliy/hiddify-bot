package tg

import (
	"context"
	"errors"
	"fmt"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/markup"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/views"
)

func (bot *Bot) handleStart(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	result, err := bot.userUC.RegisterOrGetWithState(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		bot.log.Error("start: register failed", "err", err)
		return c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}

	if result.User.IsLinked() {
		if result.FirstSeen {
			_ = c.Send("👋 Добро пожаловать! Ваш аккаунт уже привязан, показываю подключение:")
		}
		return bot.sendStatus(ctx, c)
	}

	return c.Send(
		"👋 *Привет!*\n\n"+
			"Я — ваш персональный ассистент для управления VPN-подпиской.\n\n"+
			"⚠️ Ваш Telegram пока не привязан к аккаунту. Отправьте заявку на подключение, и администратор проверит профиль.",
		tele.ModeMarkdown,
		&tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{{Text: "🔐 Запросить подключение", Data: "cmd:request_access"}},
			{{Text: "📨 Написать в поддержку", Data: "cmd:support"}},
		}},
	)
}

func (bot *Bot) handleStatus(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()
	return bot.sendStatus(ctx, c)
}

func (bot *Bot) sendStatus(ctx context.Context, c tele.Context) error {
	sub, err := bot.userUC.GetSubscription(ctx, c.Sender().ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return c.Send("❌ Аккаунт не найден. Попробуйте /start.")
		}
		return c.Send("⚠️ Не удалось получить статус. Попробуйте позже.")
	}

	return c.Send(views.Status(sub), tele.ModeMarkdown, tele.NoPreview, markup.StatusMenu())
}

func (bot *Bot) editStatus(ctx context.Context, c tele.Context) error {
	sub, err := bot.userUC.GetSubscription(ctx, c.Sender().ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return c.Send("❌ Аккаунт не найден. Попробуйте /start.")
		}
		return c.Send("⚠️ Не удалось получить статус. Попробуйте позже.")
	}

	text := views.Status(sub)

	if _, editErr := bot.b.Edit(c.Message(), text, tele.ModeMarkdown, tele.NoPreview, markup.StatusMenu()); editErr != nil {
		return c.Send(text, tele.ModeMarkdown, tele.NoPreview, markup.StatusMenu())
	}
	return nil
}

func (bot *Bot) handleAccessRequest(ctx context.Context, c tele.Context) error {
	sender := c.Sender()
	username := sender.Username

	displayName := "без username"
	if username != "" {
		displayName = "@" + username
	}

	// Use plain text for the username to avoid Markdown parse errors
	// when the username contains special characters like _ * [ ]
	adminMsg := fmt.Sprintf(
		"🔐 Заявка на подключение\n\nПользователь: %s\nTelegram ID: %d",
		displayName, sender.ID,
	)

	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		adminMsg,
		markup.AccessRequest(sender.ID),
	); err != nil {
		bot.log.Warn("access request: notify admin failed", "err", err)
		return c.Send("⚠️ Не удалось отправить заявку. Напишите в поддержку следующим сообщением.")
	}

	return c.Send("✅ Заявка отправлена. Как только администратор одобрит — вы получите уведомление и доступ к подписке.")
}
