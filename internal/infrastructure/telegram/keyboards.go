package telegram

import (
	"fmt"
	"strconv"

	tele "gopkg.in/telebot.v3"
)

func statusMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: "🔄 Обновить", Data: "cmd:status"},
		{Text: "📨 Поддержка", Data: "cmd:support"},
	}}
	return m
}

func replyMarkup(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(targetTgID, 10),
	}}}
	return m
}

func activeReplyMarkup(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: fmt.Sprintf("✍️ Ответ → %d", targetTgID), Data: "cmd:noop"},
		{Text: "Отменить", Data: "cmd:cancel_reply"},
	}}
	return m
}

func usersMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: "Все", Data: "cmd:users:all"},
		{Text: "Без TG", Data: "cmd:users:unbound"},
		{Text: "Не пишет", Data: "cmd:users:blocked"},
	}}
	return m
}
