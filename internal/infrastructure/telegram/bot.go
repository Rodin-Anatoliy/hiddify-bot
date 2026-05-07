// Package telegram implements the Telegram bot adapter.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/infrastructure/repository"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/port"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
)

const (
	handlerTimeout   = 30 * time.Second
	broadcastTimeout = 10 * time.Minute
	replyTTL         = 30 * time.Minute
)

// Bot is the Telegram adapter. It wires telebot handlers to use cases.
type Bot struct {
	b       *tele.Bot
	adminID int64
	log     *slog.Logger

	userUC      *usecase.UserUseCase
	supportUC   *usecase.SupportUseCase
	broadcastUC *usecase.BroadcastUseCase
	sessionRepo *repository.AdminSessionRepository

	mu                   sync.Mutex
	activeReplyMessageID int
}

// New constructs and configures the bot.
func New(
	token string,
	adminID int64,
	userUC *usecase.UserUseCase,
	sessionRepo *repository.AdminSessionRepository,
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

// InjectUseCases sets support and broadcast use cases after construction.
func (bot *Bot) InjectUseCases(supportUC *usecase.SupportUseCase, broadcastUC *usecase.BroadcastUseCase) {
	bot.supportUC = supportUC
	bot.broadcastUC = broadcastUC
}

func (bot *Bot) Start() {
	bot.setupCommands()
	bot.b.Start()
}
func (bot *Bot) Stop() { bot.b.Stop() }

// ── port.Sender implementation ────────────────────────────────────────────────

func (bot *Bot) SendText(ctx context.Context, telegramID int64, text string) error {
	_, err := bot.b.Send(chatByID(telegramID), text, tele.ModeMarkdown)
	if err != nil {
		bot.markUserUnreachable(ctx, telegramID, err)
	}
	return err
}

func (bot *Bot) SendPhoto(ctx context.Context, telegramID int64, fileID, caption string) error {
	_, err := bot.b.Send(chatByID(telegramID), &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}, tele.ModeMarkdown)
	if err != nil {
		bot.markUserUnreachable(ctx, telegramID, err)
	}
	return err
}

// ── Keyboards ─────────────────────────────────────────────────────────────────

// statusMenu returns the "Refresh" button shown under /status and support.
func statusMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "🔄 Обновить", Data: "cmd:status"},
			{Text: "📨 Поддержка", Data: "cmd:support"},
		},
	}
	return m
}

// replyMarkup builds the "↩️ Reply" button for admin notification messages.
func replyMarkup(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{{
			Unique: "reply_to_user",
			Text:   "↩️ Ответить",
			Data:   strconv.FormatInt(targetTgID, 10),
		}},
	}
	return m
}

func activeReplyMarkup(targetTgID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: fmt.Sprintf("✍️ Ответ → %d", targetTgID), Data: "cmd:noop"},
			{Text: "Отменить", Data: "cmd:cancel_reply"},
		},
	}
	return m
}

func usersMenu() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Все", Data: "cmd:users:all"},
			{Text: "Без TG", Data: "cmd:users:unbound"},
			{Text: "Не пишет", Data: "cmd:users:blocked"},
		},
	}
	return m
}

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
		bot.log.Warn("setCommands (users) failed", "err", err)
	}

	adminScope := tele.CommandScope{
		Type:   tele.CommandScopeChat,
		ChatID: bot.adminID,
	}
	if err := bot.b.SetCommands(adminCommands, adminScope); err != nil {
		bot.log.Warn("setCommands (admin) failed", "err", err)
	}

	bot.log.Info("bot commands registered")
}

// ── Handler registration ──────────────────────────────────────────────────────

var replyBtn = tele.InlineButton{Unique: "reply_to_user"}

func (bot *Bot) registerHandlers() {
	bot.b.Use(func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			defer func() {
				if r := recover(); r != nil {
					var err error
					switch t := r.(type) {
					case error:
						err = t
					default:
						err = fmt.Errorf("%v", t)
					}
					bot.log.Error("handler panic",
						"err", err,
						"sender", c.Sender().ID,
						"update_id", c.Update().ID,
					)
				}
			}()
			return next(c)
		}
	})

	// User commands.
	bot.b.Handle("/start", bot.handleStart)
	bot.b.Handle("/status", bot.handleStatus)
	bot.b.Handle("/support", bot.handleSupportPrompt)

	// Admin-only commands.
	admin := bot.b.Group()
	admin.Use(bot.adminOnly)
	admin.Handle("/broadcast", bot.handleBroadcast)
	admin.Handle("/bind", bot.handleBind)
	admin.Handle("/sync", bot.handleSync)
	admin.Handle("/users", bot.handleUsers)
	admin.Handle("/history", bot.handleHistory)
	admin.Handle("/cancel", bot.handleCancelReply)

	// Inline button callbacks.
	bot.b.Handle(&replyBtn, bot.handleReplyCallback)
	bot.b.Handle(tele.OnCallback, bot.handleCallback)

	// Free-text and photos — routed by sender role.
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

// ── User handlers ─────────────────────────────────────────────────────────────

func (bot *Bot) handleStart(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	result, err := bot.userUC.RegisterOrGetWithState(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		bot.log.Error("start: register failed", "err", err)
		return c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}
	u := result.User

	if u.IsLinked() {
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

// sendStatus is the shared logic for /status command and "🔄 Обновить" callback.
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

// editStatus edits the existing message in place (used by "🔄 Обновить" callback).
// Falls back to sending a new message if editing fails (e.g. message too old).
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

	_, editErr := bot.b.Edit(c.Message(), text, tele.ModeMarkdown, tele.NoPreview, statusMenu())
	if editErr != nil {
		// Message may be too old to edit — fall back to new message.
		return c.Send(text, tele.ModeMarkdown, tele.NoPreview, statusMenu())
	}
	return nil
}

func (bot *Bot) handleSupportPrompt(c tele.Context) error {
	return c.Send("📨 Напишите ваш вопрос следующим сообщением — ответим как можно скорее.")
}

// ── Inline callback router ────────────────────────────────────────────────────

// handleCallback routes generic inline button presses (cmd:*).
func (bot *Bot) handleCallback(c tele.Context) error {
	_ = c.Respond() // acknowledge immediately to remove loading spinner

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

// ── Support messaging ─────────────────────────────────────────────────────────

func (bot *Bot) routeText(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		return bot.handleAdminReply(c)
	}
	return bot.handleUserText(c)
}

func (bot *Bot) routePhoto(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		return nil // admin photo replies not yet implemented
	}
	return bot.handleUserPhoto(c)
}

func (bot *Bot) handleUserText(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if _, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Text(),
	}); err != nil {
		bot.log.Error("support: handle text", "err", err)
		return c.Send("⚠️ Не удалось доставить сообщение. Попробуйте позже.")
	}

	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		fmt.Sprintf("📩 *@%s* (`%d`)\n\n%s", c.Sender().Username, c.Sender().ID, c.Text()),
		tele.ModeMarkdown,
		replyMarkup(c.Sender().ID),
	); err != nil {
		bot.log.Warn("support: notify admin failed", "err", err)
	}

	return c.Send("✅ Сообщение получено — ответим скоро!")
}

func (bot *Bot) handleUserPhoto(c tele.Context) error {
	photo := c.Message().Photo
	if photo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if _, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Message().Caption,
		FileID:     photo.FileID,
	}); err != nil {
		bot.log.Error("support: handle photo", "err", err)
		return c.Send("⚠️ Не удалось доставить фото.")
	}

	fwdText := fmt.Sprintf("📸 *@%s* (`%d`) прислал фото.\n\n%s",
		c.Sender().Username, c.Sender().ID, c.Message().Caption)
	if _, err := bot.b.Send(
		chatByID(bot.adminID),
		&tele.Photo{File: tele.File{FileID: photo.FileID}, Caption: fwdText},
		tele.ModeMarkdown,
		replyMarkup(c.Sender().ID),
	); err != nil {
		bot.log.Warn("support: notify admin (photo) failed", "err", err)
	}

	return c.Send("✅ Фото получено!")
}

// ── Admin reply flow ──────────────────────────────────────────────────────────

func (bot *Bot) handleReplyCallback(c tele.Context) error {
	targetID, err := strconv.ParseInt(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Неверный ID пользователя"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if err := bot.sessionRepo.Save(ctx, repository.AdminSession{
		MessageID:  int(c.Message().ID),
		TargetTgID: targetID,
		ExpiresAt:  time.Now().Add(replyTTL),
	}); err != nil {
		bot.log.Error("session save failed", "err", err)
	}
	if err := bot.sessionRepo.Save(ctx, repository.AdminSession{
		MessageID:  activeAdminReplySessionID(bot.adminID),
		TargetTgID: targetID,
		ExpiresAt:  time.Now().Add(replyTTL),
	}); err != nil {
		bot.log.Error("active session save failed", "err", err)
	}
	bot.setActiveReplyMessageID(c.Message().ID)

	_ = c.Respond(&tele.CallbackResponse{
		Text: fmt.Sprintf("Следующее сообщение уйдёт пользователю %d", targetID),
	})
	if _, err := bot.b.EditReplyMarkup(c.Message(), activeReplyMarkup(targetID)); err != nil {
		bot.log.Warn("active reply markup failed", "err", err)
	}
	return nil
}

func (bot *Bot) handleAdminReply(c tele.Context) error {
	if strings.HasPrefix(c.Text(), "/") {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	session, sessionKey, err := bot.findAdminReplySession(ctx, c)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		bot.log.Error("session get failed", "err", err)
		return nil
	}

	if err := bot.sessionRepo.Delete(ctx, sessionKey); err != nil {
		bot.log.Warn("session delete failed", "err", err)
	}
	if sessionKey != activeAdminReplySessionID(bot.adminID) {
		if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
			bot.log.Warn("active session delete failed", "err", err)
		}
	}

	if _, err := bot.supportUC.HandleAdminReply(ctx, session.TargetTgID, c.Text(), ""); err != nil {
		bot.log.Error("support: admin reply failed", "err", err)
		return c.Send("⚠️ Не удалось отправить ответ.")
	}
	if sessionKey == activeAdminReplySessionID(bot.adminID) {
		bot.restoreActiveReplyMarkup(session.TargetTgID)
	}
	return c.Send(
		fmt.Sprintf("✅ Ответ отправлен пользователю `%d`.", session.TargetTgID),
		tele.ModeMarkdown,
	)
}

func (bot *Bot) handleCancelReply(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
		bot.log.Warn("active session delete failed", "err", err)
		return c.Send("⚠️ Не удалось отменить активный ответ.")
	}
	bot.clearActiveReplyMessageID()
	return c.Send("✅ Активный ответ отменён.")
}

func (bot *Bot) cancelAdminReply(ctx context.Context, c tele.Context) error {
	session, err := bot.sessionRepo.Get(ctx, c.Message().ID)
	if err != nil && !errors.Is(err, apperr.ErrNotFound) {
		bot.log.Warn("reply session get failed", "err", err)
	}
	if err := bot.sessionRepo.Delete(ctx, activeAdminReplySessionID(bot.adminID)); err != nil {
		bot.log.Warn("active session delete failed", "err", err)
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось отменить"})
	}
	bot.clearActiveReplyMessageID()
	if session != nil {
		if _, err := bot.b.EditReplyMarkup(c.Message(), replyMarkup(session.TargetTgID)); err != nil {
			bot.log.Warn("reply markup restore failed", "err", err)
		}
	}
	return c.Respond(&tele.CallbackResponse{Text: "Ответ отменён"})
}

func (bot *Bot) findAdminReplySession(ctx context.Context, c tele.Context) (*repository.AdminSession, int, error) {
	if replyTo := c.Message().ReplyTo; replyTo != nil {
		session, err := bot.sessionRepo.Get(ctx, replyTo.ID)
		if err == nil {
			return session, replyTo.ID, nil
		}
		if !errors.Is(err, apperr.ErrNotFound) {
			return nil, 0, err
		}
	}

	key := activeAdminReplySessionID(bot.adminID)
	session, err := bot.sessionRepo.Get(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	return session, key, nil
}

// ── Admin commands ────────────────────────────────────────────────────────────

func (bot *Bot) handleBroadcast(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), broadcastTimeout)
	defer cancel()

	var msg usecase.BroadcastMessage
	if photo := c.Message().Photo; photo != nil {
		msg.FileID = photo.FileID
		msg.Text = c.Message().Caption
	} else {
		msg.Text = strings.TrimPrefix(c.Text(), "/broadcast ")
		if msg.Text == "" || msg.Text == "/broadcast" {
			return c.Send("Использование:\n/broadcast <текст>\nили /broadcast с прикреплённым фото")
		}
	}

	_ = c.Send("📤 Рассылка запущена...")

	result, err := bot.broadcastUC.Send(ctx, msg)
	if err != nil {
		return c.Send("⚠️ Ошибка рассылки: " + err.Error())
	}
	return c.Send(fmt.Sprintf(
		"✅ Готово\n\nВсего: %d\n✔️ Доставлено: %d\n❌ Ошибок: %d",
		result.Total, result.Success, result.Failed,
	))
}

// handleBind: /bind <telegram_id> <hiddify_uuid>
func (bot *Bot) handleBind(c tele.Context) error {
	parts := strings.Fields(c.Text())
	if len(parts) != 3 {
		return c.Send("Использование: /bind <telegram_id> <hiddify_uuid>")
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id — должно быть число.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if err := bot.userUC.LinkManually(ctx, targetID, parts[2]); err != nil {
		return c.Send("⚠️ Ошибка привязки: " + err.Error())
	}
	return c.Send(
		fmt.Sprintf(
			"✅ Пользователь `%d` привязан к UUID `%s`.\n\nЕсли ещё не запускал бота — попросите нажать /start.",
			targetID, parts[2],
		),
		tele.ModeMarkdown,
	)
}

func (bot *Bot) handleSync(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	result, err := bot.userUC.SyncFromHiddify(ctx)
	if err != nil {
		return c.Send("⚠️ Ошибка синхронизации: " + err.Error())
	}
	return c.Send(fmt.Sprintf(
		"✅ Синхронизация завершена\n\n"+
			"Всего в панели: %d\n"+
			"С telegram\\_id: %d\n"+
			"Создано: %d\n"+
			"Обновлено: %d\n"+
			"Пропущено: %d",
		result.Total, result.Linked, result.Created, result.Updated, result.Skipped,
	), tele.ModeMarkdown)
}

// handleUsers:
// /users — all Hiddify users enriched with local bot state
// /users unbound — Hiddify users without telegram_id
// /users blocked — locally linked users the bot cannot message
func (bot *Bot) handleUsers(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	parts := strings.Fields(c.Text())
	if len(parts) > 1 {
		switch parts[1] {
		case "unbound", "unlinked":
			return bot.showUsers(ctx, c, "unbound", false)
		case "blocked", "inactive":
			return bot.showUsers(ctx, c, "blocked", false)
		default:
			return c.Send("Использование:\n/users\n/users unbound\n/users blocked")
		}
	}

	return bot.showUsers(ctx, c, "all", false)
}

func (bot *Bot) showUsers(ctx context.Context, c tele.Context, mode string, edit bool) error {
	var text string
	var err error

	switch mode {
	case "all":
		text, err = bot.usersAllText(ctx)
	case "unbound":
		text, err = bot.usersUnboundText(ctx)
	case "blocked":
		text, err = bot.usersBlockedText(ctx)
	default:
		text = "Использование:\n/users\n/users unbound\n/users blocked"
	}
	if err != nil {
		text = "⚠️ Ошибка получения списка: " + err.Error()
	}

	if edit {
		if _, editErr := bot.b.Edit(c.Message(), text, usersMenu()); editErr == nil {
			return nil
		}
	}
	return c.Send(text, usersMenu())
}

func (bot *Bot) usersAllText(ctx context.Context) (string, error) {
	users, err := bot.userUC.ListPanelUserViews(ctx)
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "В Hiddify нет пользователей.", nil
	}

	text := formatPanelUsers("👥 Пользователи Hiddify", users)
	return text, nil
}

func (bot *Bot) usersBlockedText(ctx context.Context) (string, error) {
	users, err := bot.userUC.ListLinked(ctx)
	if err != nil {
		return "", err
	}
	blocked := make([]*user.User, 0)
	for _, u := range users {
		if !u.CanMessage {
			blocked = append(blocked, u)
		}
	}
	if len(blocked) == 0 {
		return "Нет привязанных пользователей, недоступных для сообщений.", nil
	}
	return formatLocalUsers("🚫 Не получают сообщения", blocked), nil
}

func (bot *Bot) usersUnboundText(ctx context.Context) (string, error) {
	panelUsers, err := bot.userUC.ListUnboundPanelUsers(ctx)
	if err != nil {
		return "", err
	}
	if len(panelUsers) == 0 {
		return "В Hiddify нет пользователей без telegram_id.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "🔎 Hiddify без telegram_id: %d\n\n", len(panelUsers))

	const pageSize = 50
	for i, u := range panelUsers {
		if i >= pageSize {
			fmt.Fprintf(&sb, "\n...и ещё %d. Показаны первые %d.", len(panelUsers)-pageSize, pageSize)
			break
		}
		name := u.Name
		if name == "" {
			name = "—"
		}
		fmt.Fprintf(&sb, "%d. %s | uuid: %s\n", i+1, name, shortID(u.UUID))
	}
	sb.WriteString("\nДля привязки: /bind <telegram_id> <uuid>")

	return sb.String(), nil
}

// handleHistory: /history <telegram_id> — last N support messages for a user.
func (bot *Bot) handleHistory(c tele.Context) error {
	parts := strings.Fields(c.Text())
	if len(parts) != 2 {
		return c.Send("Использование: /history <telegram_id>")
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	msgs, err := bot.supportUC.GetHistory(ctx, targetID)
	if err != nil {
		return c.Send("⚠️ Ошибка получения истории: " + err.Error())
	}
	if len(msgs) == 0 {
		return c.Send("Сообщений от этого пользователя нет.")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 *История для* `%d`\n\n", targetID)

	const maxMessages = 20
	start := 0
	if len(msgs) > maxMessages {
		start = len(msgs) - maxMessages
		fmt.Fprintf(&sb, "_Показаны последние %d из %d_\n\n", maxMessages, len(msgs))
	}

	for _, m := range msgs[start:] {
		dir := "👤"
		if m.Direction == ticket.DirectionAdminToUser {
			dir = "🔧"
		}
		ts := m.CreatedAt.Format("02.01 15:04")
		text := m.Text
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		if text == "" {
			text = "[фото]"
		}
		fmt.Fprintf(&sb, "%s `%s`\n%s\n\n", dir, ts, text)
	}

	return c.Send(sb.String(), tele.ModeMarkdown)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

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
	if _, err := bot.b.EditReplyMarkup(msg, replyMarkup(targetTgID)); err != nil {
		bot.log.Warn("reply markup restore failed", "err", err)
	}
}

func (bot *Bot) markUserUnreachable(ctx context.Context, telegramID int64, sendErr error) {
	if !isUserUnreachableError(sendErr) {
		return
	}
	if err := bot.userUC.MarkCanMessage(ctx, telegramID, false); err != nil {
		bot.log.Warn("mark user unreachable failed", "telegram_id", telegramID, "err", err)
		return
	}
	bot.log.Info("user marked unreachable", "telegram_id", telegramID, "send_err", sendErr)
}

func isUserUnreachableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "bot was blocked") ||
		strings.Contains(msg, "chat not found") ||
		strings.Contains(msg, "user is deactivated")
}

func formatLocalUsers(title string, users []*user.User) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %d\n\n", title, len(users))

	const pageSize = 50
	for i, u := range users {
		if i >= pageSize {
			fmt.Fprintf(&sb, "\n...и ещё %d. Показаны первые %d.", len(users)-pageSize, pageSize)
			break
		}
		canMsg := "нет"
		if u.CanMessage {
			canMsg = "да"
		}
		name := u.Username
		if name == "" {
			name = "—"
		} else {
			name = "@" + name
		}
		fmt.Fprintf(&sb, "%d. %s | tg: %d | msg: %s | uuid: %s\n",
			i+1, name, u.TelegramID, canMsg, shortID(u.HiddifyUUID))
	}
	sb.WriteString("\nmsg: да — бот может писать; msg: нет — не запускал, заблокировал или доставка падала.")
	return sb.String()
}

func formatPanelUsers(title string, users []usecase.PanelUserView) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %d\n\n", title, len(users))

	const pageSize = 50
	for i, u := range users {
		if i >= pageSize {
			fmt.Fprintf(&sb, "\n...и ещё %d. Показаны первые %d.", len(users)-pageSize, pageSize)
			break
		}
		name := u.Name
		if name == "" {
			name = "—"
		}
		tg := "нет"
		botState := "нет tg"
		if u.TelegramID != nil && *u.TelegramID != 0 {
			tg = strconv.FormatInt(*u.TelegramID, 10)
			switch {
			case u.CanMessage:
				botState = "пишет"
			case u.KnownToBot:
				botState = "не пишет"
			default:
				botState = "не запускал"
			}
		}
		fmt.Fprintf(&sb, "%d. %s | tg: %s | bot: %s | uuid: %s\n",
			i+1, name, tg, botState, shortID(u.UUID))
	}
	sb.WriteString("\nbot: пишет — бот может отправлять; не пишет — заблокирован/доставка падала; не запускал — tg есть в панели, но /start не было.")
	return sb.String()
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func formatBytes(b int64) string {
	if b < 0 {
		return "∞"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Compile-time check: *Bot must satisfy port.Sender.
var _ port.Sender = (*Bot)(nil)
