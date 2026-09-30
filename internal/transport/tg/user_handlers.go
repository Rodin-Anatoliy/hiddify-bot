package tg

import (
	"context"
	"errors"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/markup"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/views"
)

func (bot *Bot) handleStart(c tele.Context) error {
	// Deep link t.me/<bot>?start=i_<CODE>. Redeem first, before RegisterOrGet:
	// its auto-link would otherwise grab a panel user created by an earlier
	// timed-out attempt and leave the invite unfinished. Any other payload
	// (or none) goes to the usual flow.
	if msg := c.Message(); msg != nil && strings.HasPrefix(msg.Payload, startPayloadPrefix) {
		alreadySubscribed, err := bot.redeemAndReply(c, strings.TrimPrefix(msg.Payload, startPayloadPrefix), false)
		if !alreadySubscribed {
			return err
		}
	}

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
			"⚠️ Ваш Telegram пока не привязан к подписке. Если у вас есть код приглашения — нажмите «У меня есть код». Кода нет — попросите его у администратора.",
		tele.ModeMarkdown,
		markup.UnlinkedMenu(),
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
