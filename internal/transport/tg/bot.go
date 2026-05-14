// package tg adapts Telegram updates to application use cases.
package tg

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/admin"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
)

const (
	handlerTimeout   = 30 * time.Second
	broadcastTimeout = 10 * time.Minute
	replyTTL         = 30 * time.Minute
)

// Bot wires Telegram handlers to use cases.
type Bot struct {
	b       *tele.Bot
	adminID int64
	log     *slog.Logger

	userUC      *service.UserUseCase
	supportUC   *service.SupportUseCase
	broadcastUC *service.BroadcastUseCase
	sessionRepo admin.SessionRepository

	mu                   sync.Mutex
	activeReplyMessageID int
}

func New(
	token string,
	adminID int64,
	userUC *service.UserUseCase,
	sessionRepo admin.SessionRepository,
	log *slog.Logger,
) (*Bot, error) {
	b, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("telegram: new bot: %w", err)
	}

	bot := &Bot{
		b:           b,
		adminID:     adminID,
		log:         log.With("component", "telegram"),
		userUC:      userUC,
		sessionRepo: sessionRepo,
	}
	bot.registerHandlers()
	return bot, nil
}

func (bot *Bot) InjectUseCases(supportUC *service.SupportUseCase, broadcastUC *service.BroadcastUseCase) {
	bot.supportUC = supportUC
	bot.broadcastUC = broadcastUC
}

func (bot *Bot) Start() {
	bot.setupCommands()
	bot.b.Start()
}

func (bot *Bot) Stop() { bot.b.Stop() }

func (bot *Bot) setupCommands() {
	userCommands := []tele.Command{
		{Text: "status", Description: "Статус подписки"},
	}

	adminCommands := []tele.Command{
		{Text: "status", Description: "Статус подписки"},
		{Text: "broadcast", Description: "Рассылка всем пользователям"},
		{Text: "sync", Description: "Синхронизация с панелью Hiddify"},
		{Text: "users", Description: "Пользователи Hiddify и Telegram-статус"},
		{Text: "bind", Description: "Привязать пользователя (tg_id uuid)"},
		{Text: "history", Description: "История обращений пользователя"},
	}

	if err := bot.b.SetCommands(userCommands); err != nil {
		bot.log.Warn("set user commands failed", "err", err)
	}

	adminScope := tele.CommandScope{
		Type:   tele.CommandScopeChat,
		ChatID: bot.adminID,
	}
	if err := bot.b.SetCommands(adminCommands, adminScope); err != nil {
		bot.log.Warn("set admin commands failed", "err", err)
	}
}

var replyBtn = tele.InlineButton{Unique: "reply_to_user"}

func (bot *Bot) registerHandlers() {
	bot.b.Use(func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			defer func() {
				if r := recover(); r != nil {
					bot.log.Error("handler panic",
						"err", fmt.Errorf("%v", r),
						"sender", c.Sender().ID,
						"update_id", c.Update().ID,
					)
				}
			}()
			return next(c)
		}
	})

	bot.b.Handle("/start", bot.handleStart)
	bot.b.Handle("/status", bot.handleStatus)
	bot.b.Handle("/support", bot.handleSupportPrompt)

	admin := bot.b.Group()
	admin.Use(bot.adminOnly)
	admin.Handle("/broadcast", bot.handleBroadcast)
	admin.Handle("/bind", bot.handleBind)
	admin.Handle("/sync", bot.handleSync)
	admin.Handle("/users", bot.handleUsers)
	admin.Handle("/history", bot.handleHistory)
	admin.Handle("/cancel", bot.handleCancelReply)

	bot.b.Handle(&replyBtn, bot.handleReplyCallback)
	bot.b.Handle(tele.OnCallback, bot.handleCallback)
	bot.b.Handle(tele.OnText, bot.routeText)
	bot.b.Handle(tele.OnPhoto, bot.routePhoto)
}

func (bot *Bot) adminOnly(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if c.Sender().ID != bot.adminID {
			return nil
		}
		return next(c)
	}
}
