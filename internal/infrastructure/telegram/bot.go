// Package telegram implements the Telegram bot adapter.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
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

func (bot *Bot) SendText(_ context.Context, telegramID int64, text string) error {
	_, err := bot.b.Send(chatByID(telegramID), text, tele.ModeMarkdown)
	return err
}

func (bot *Bot) SendPhoto(_ context.Context, telegramID int64, fileID, caption string) error {
	_, err := bot.b.Send(chatByID(telegramID), &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}, tele.ModeMarkdown)
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

func (bot *Bot) setupCommands() {
	userCommands := []tele.Command{
		{Text: "status", Description: "Статус подписки"},
	}

	adminCommands := []tele.Command{
		{Text: "status", Description: "Статус подписки"},
		{Text: "broadcast", Description: "Рассылка всем пользователям"},
		{Text: "sync", Description: "Синхронизация с панелью Hiddify"},
		{Text: "users", Description: "Список привязанных пользователей"},
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

	u, err := bot.userUC.RegisterOrGet(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		bot.log.Error("start: register failed", "err", err)
		return c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}

	if u.IsLinked() {
		_ = c.Send("👋 С возвращением! Текущее состояние вашего подключения:")
		return bot.sendStatus(ctx, c)
	}

	return c.Send(
		"👋 *Привет!*\n\n"+
			"Я — ваш персональный ассистент для управления VPN-подпиской.\n\n"+
			"⚠️ Ваш аккаунт пока не привязан. Нажмите кнопку ниже, чтобы связаться с администратором и получить доступ.",
		tele.ModeMarkdown,
		&tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
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
	}
	return nil
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

	prompt, err := bot.b.Send(
		chatByID(bot.adminID),
		fmt.Sprintf("✏️ Ответьте *reply* на это сообщение для пользователя `%d`:", targetID),
		tele.ModeMarkdown,
	)
	if err != nil {
		bot.log.Error("failed to send reply prompt", "err", err)
		return c.Respond(&tele.CallbackResponse{Text: "Ошибка отправки промпта"})
	}

	if err := bot.sessionRepo.Save(ctx, repository.AdminSession{
		MessageID:  prompt.ID,
		TargetTgID: targetID,
		ExpiresAt:  time.Now().Add(replyTTL),
	}); err != nil {
		bot.log.Error("session save failed", "err", err)
	}

	_ = c.Respond()
	return nil
}

func (bot *Bot) handleAdminReply(c tele.Context) error {
	replyTo := c.Message().ReplyTo
	if replyTo == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	session, err := bot.sessionRepo.Get(ctx, replyTo.ID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		bot.log.Error("session get failed", "err", err)
		return nil
	}

	if err := bot.sessionRepo.Delete(ctx, replyTo.ID); err != nil {
		bot.log.Warn("session delete failed", "err", err)
	}

	if _, err := bot.supportUC.HandleAdminReply(ctx, session.TargetTgID, c.Text(), ""); err != nil {
		bot.log.Error("support: admin reply failed", "err", err)
		return c.Send("⚠️ Не удалось отправить ответ.")
	}
	return c.Send(
		fmt.Sprintf("✅ Ответ отправлен пользователю `%d`.", session.TargetTgID),
		tele.ModeMarkdown,
	)
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

// handleUsers: /users — list of all linked users with messaging status.
func (bot *Bot) handleUsers(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	users, err := bot.userUC.ListLinked(ctx)
	if err != nil {
		return c.Send("⚠️ Ошибка получения списка: " + err.Error())
	}
	if len(users) == 0 {
		return c.Send("Нет привязанных пользователей.")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "👥 *Привязано: %d*\n\n", len(users))

	const pageSize = 50
	for i, u := range users {
		if i >= pageSize {
			fmt.Fprintf(&sb, "\n_...и ещё %d. Показаны первые %d._", len(users)-pageSize, pageSize)
			break
		}
		canMsg := "❌"
		if u.CanMessage {
			canMsg = "✅"
		}
		name := u.Username
		if name == "" {
			name = "—"
		}
		fmt.Fprintf(&sb, "%d. @%s `%d` %s\n", i+1, name, u.TelegramID, canMsg)
	}
	sb.WriteString("\n✅ — получает рассылки  ❌ — не запускал бота")

	return c.Send(sb.String(), tele.ModeMarkdown)
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
