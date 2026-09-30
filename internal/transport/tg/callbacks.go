package tg

import (
	"context"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/markup"
)

func (bot *Bot) handleCallback(c tele.Context) error {
	_ = c.Respond()

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	data := c.Data()

	switch {
	case strings.HasPrefix(data, "approve:"):
		return bot.handleApproveAccess(c)
	case strings.HasPrefix(data, "reject:"):
		return bot.handleRejectAccess(c)
	case strings.HasPrefix(data, "inv_"):
		return bot.handleInviteCallback(c)
	}

	switch data {
	case "cmd:status":
		return bot.editStatus(ctx, c)
	case "cmd:support":
		bot.codeWait.clear(c.Sender().ID) // the next text is a support message, not a code
		return c.Send("📨 Напишите ваш вопрос следующим сообщением — ответим как можно скорее.")
	case "cmd:have_code":
		return bot.handleHaveCode(c)
	case "cmd:request_access":
		// Old menus may still show the button; requests are replaced by codes.
		return c.Send("🎟 Чтобы подключиться, нужен код приглашения. Попросите его у администратора.", markup.UnlinkedMenu())
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
