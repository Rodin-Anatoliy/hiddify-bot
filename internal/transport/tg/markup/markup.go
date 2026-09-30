package markup

import (
	"fmt"
	"strconv"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
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

// Invite wizard (admin). Callback data: inv_kind:<kind>, inv_days:<kind>:<days>, inv_revoke:<id>.

func InviteKind() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{
		{Text: "Свой", Data: "inv_kind:" + string(invite.KindOwn)},
		{Text: "Знакомый", Data: "inv_kind:" + string(invite.KindFriend)},
	}}
	return m
}

var inviteDayButtons = []struct {
	days  int
	label string
}{
	{1, "День"}, {7, "Неделя"}, {30, "Месяц"}, {180, "Полгода"}, {invite.Unlimited, "Бессрочно"},
}

// InviteDays lists the terms; the default one carries a mark.
func InviteDays(kind invite.Kind) *tele.ReplyMarkup {
	row := make([]tele.InlineButton, 0, len(inviteDayButtons))
	for _, b := range inviteDayButtons {
		text := b.label
		if b.days == invite.DefaultDays {
			text = "✅ " + text
		}
		row = append(row, tele.InlineButton{Text: text, Data: fmt.Sprintf("inv_days:%s:%d", kind, b.days)})
	}
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{row[:3], row[3:]}
	return m
}

// InviteShare is the «Переслать» button under a freshly issued code.
func InviteShare(shareURL string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{{{Text: "📤 Переслать", URL: shareURL}}}
	return m
}

// InviteRevokeList has one «Отозвать» button per invite id.
func InviteRevokeList(ids []int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	for _, id := range ids {
		m.InlineKeyboard = append(m.InlineKeyboard, []tele.InlineButton{{
			Text: fmt.Sprintf("Отозвать #%d", id),
			Data: fmt.Sprintf("inv_revoke:%d", id),
		}})
	}
	return m
}

// UnlinkedMenu is the menu of a person without a subscription.
func UnlinkedMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{{Text: "🎟 У меня есть код", Data: "cmd:have_code"}},
		{{Text: "📨 Написать в поддержку", Data: "cmd:support"}},
	}
	return m
}
