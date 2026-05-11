package telegram

import (
	"context"
	"errors"
	"fmt"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/errs"
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
		if errors.Is(err, errs.ErrNotFound) {
			return c.Send("❌ Аккаунт не найден. Попробуйте /start.")
		}
		return c.Send("⚠️ Не удалось получить статус. Попробуйте позже.")
	}

	statusLabel := "🟢 Активна"
	if !sub.IsActive || sub.IsExpired() {
		statusLabel = "🔴 Неактивна"
	}

	remaining := "∞"
	if sub.TotalTrafficBytes > 0 {
		remaining = formatBytes(sub.RemainingTrafficBytes())
	}

	expire := "Бессрочно"
	if sub.ExpireDate != nil {
		expire = sub.ExpireDate.Format("02.01.2006")
	}

	text := fmt.Sprintf(
		"📊 *Статус подписки*\n\n"+
			"Статус: %s\n"+
			"Использовано: %s\n"+
			"Остаток: %s\n"+
			"Истекает: %s\n\n"+
			"🔗 [Ссылка на подписку](%s)",
		statusLabel,
		formatBytes(sub.UsedTrafficBytes),
		remaining,
		expire,
		sub.SubscriptionURL,
	)
	return c.Send(text, tele.ModeMarkdown, tele.NoPreview, statusMenu())
}

func (bot *Bot) editStatus(ctx context.Context, c tele.Context) error {
	sub, err := bot.userUC.GetSubscription(ctx, c.Sender().ID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return c.Send("❌ Аккаунт не найден. Попробуйте /start.")
		}
		return c.Send("⚠️ Не удалось получить статус. Попробуйте позже.")
	}

	statusLabel := "🟢 Активна"
	if !sub.IsActive || sub.IsExpired() {
		statusLabel = "🔴 Неактивна"
	}

	remaining := "∞"
	if sub.TotalTrafficBytes > 0 {
		remaining = formatBytes(sub.RemainingTrafficBytes())
	}

	expire := "Бессрочно"
	if sub.ExpireDate != nil {
		expire = sub.ExpireDate.Format("02.01.2006")
	}

	text := fmt.Sprintf(
		"📊 *Статус подписки*\n\n"+
			"Статус: %s\n"+
			"Использовано: %s\n"+
			"Остаток: %s\n"+
			"Истекает: %s\n\n"+
			"🔗 [Ссылка на подписку](%s)",
		statusLabel,
		formatBytes(sub.UsedTrafficBytes),
		remaining,
		expire,
		sub.SubscriptionURL,
	)

	if _, editErr := bot.b.Edit(c.Message(), text, tele.ModeMarkdown, tele.NoPreview, statusMenu()); editErr != nil {
		return c.Send(text, tele.ModeMarkdown, tele.NoPreview, statusMenu())
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

	// Build approve button — encodes telegramID:username so handler has all data.
	// Only encode telegram_id — username fetched from local DB on approval.
	approveData := fmt.Sprintf("approve:%d", sender.ID)
	approveMarkup := &tele.ReplyMarkup{
		InlineKeyboard: [][]tele.InlineButton{
			{{Text: "✅ Одобрить и создать аккаунт", Data: approveData}},
			{{Text: "❌ Отклонить", Data: fmt.Sprintf("reject:%d", sender.ID)}},
		},
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
		approveMarkup,
	); err != nil {
		bot.log.Warn("access request: notify admin failed", "err", err)
		return c.Send("⚠️ Не удалось отправить заявку. Напишите в поддержку следующим сообщением.")
	}

	return c.Send("✅ Заявка отправлена. Как только администратор одобрит — вы получите уведомление и доступ к подписке.")
}
