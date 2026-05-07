package telegram

import (
	"context"
	"errors"
	"fmt"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
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
		if errors.Is(err, apperr.ErrNotFound) {
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
		if errors.Is(err, apperr.ErrNotFound) {
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
	if username == "" {
		username = "без username"
	} else {
		username = "@" + username
	}

	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		fmt.Sprintf(
			"🔐 *Заявка на подключение*\n\nПользователь: %s\nTelegram ID: `%d`\n\nПосле проверки создайте или найдите пользователя в Hiddify и выполните:\n`/bind %d <hiddify_uuid>`",
			username,
			sender.ID,
			sender.ID,
		),
		tele.ModeMarkdown,
	); err != nil {
		bot.log.Warn("access request: notify admin failed", "err", err)
		return c.Send("⚠️ Не удалось отправить заявку. Напишите в поддержку следующим сообщением.")
	}

	return c.Send("✅ Заявка отправлена администратору. После проверки доступ привяжут к этому Telegram.")
}
