package markup

import (
	"fmt"
	"strconv"

	tele "gopkg.in/telebot.v3"
)

func StatusMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: "🔄 Обновить", Data: "cmd:status"},
		{Text: "📨 Поддержка", Data: "cmd:support"},
	}}
	return m
}

func Reply(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(targetTgID, 10),
	}}}
	return m
}

func ActiveReply(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: fmt.Sprintf("✍️ Ответ → %d", targetTgID), Data: "cmd:noop"},
		{Text: "Отменить", Data: "cmd:cancel_reply"},
	}}
	return m
}

func UsersMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: "Все", Data: "cmd:users:all"},
		{Text: "Без TG", Data: "cmd:users:unbound"},
		{Text: "Не пишет", Data: "cmd:users:blocked"},
	}}
	return m
}

func AccessRequest(telegramID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{{Text: "✅ Одобрить и создать аккаунт", Data: fmt.Sprintf("approve:%d", telegramID)}},
		{{Text: "❌ Отклонить", Data: fmt.Sprintf("reject:%d", telegramID)}},
	}
	return m
}
