package tg

import (
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/markup"

	tele "gopkg.in/telebot.v3"
)

func chatByID(id int64) *tele.Chat { return &tele.Chat{ID: id} }

func activeAdminReplySessionID(adminID int64) int {
	if adminID > 0 {
		return -int(adminID)
	}
	return -1
}

func (bot *Bot) setActiveReplyMessageID(messageID int) {
	bot.mu.Lock()
	defer bot.mu.Unlock()
	bot.activeReplyMessageID = messageID
}

func (bot *Bot) clearActiveReplyMessageID() {
	bot.mu.Lock()
	defer bot.mu.Unlock()
	bot.activeReplyMessageID = 0
}

func (bot *Bot) restoreActiveReplyMarkup(targetTgID int64) {
	bot.mu.Lock()
	messageID := bot.activeReplyMessageID
	bot.activeReplyMessageID = 0
	bot.mu.Unlock()

	if messageID == 0 {
		return
	}
	msg := &tele.Message{
		ID:   messageID,
		Chat: chatByID(bot.adminID),
	}
	if _, err := bot.b.EditReplyMarkup(msg, markup.Reply(targetTgID)); err != nil {
		bot.log.Warn("reply markup restore failed", "err", err)
	}
}
