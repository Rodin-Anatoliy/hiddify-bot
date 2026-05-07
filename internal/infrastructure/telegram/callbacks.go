package telegram

import (
	"context"

	tele "gopkg.in/telebot.v3"
)

func (bot *Bot) handleCallback(c tele.Context) error {
	_ = c.Respond()

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	switch c.Data() {
	case "cmd:status":
		return bot.editStatus(ctx, c)
	case "cmd:support":
		return c.Send("📨 Напишите ваш вопрос следующим сообщением — ответим как можно скорее.")
	case "cmd:request_access":
		return bot.handleAccessRequest(ctx, c)
	case "cmd:cancel_reply":
		return bot.cancelAdminReply(ctx, c)
	case "cmd:users:all":
		return bot.showUsers(ctx, c, "all", true)
	case "cmd:users:unbound":
		return bot.showUsers(ctx, c, "unbound", true)
	case "cmd:users:blocked":
		return bot.showUsers(ctx, c, "blocked", true)
	case "cmd:noop":
		return nil
	}
	return nil
}
