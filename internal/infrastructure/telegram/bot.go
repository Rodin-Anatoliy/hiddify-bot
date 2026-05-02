// Package telegram implements the Telegram bot adapter: handler registration and message sending.
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
	"gopkg.in/telebot.v3/middleware"

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

	// sessionRepo persists reply context across restarts.
	// When admin clicks "Reply", we store which user they're replying to in SQLite.
	sessionRepo *repository.AdminSessionRepository
}

// New constructs and configures the bot. Use cases can be injected later via InjectUseCases.
func New(token string, adminID int64, userUC *usecase.UserUseCase, sessionRepo *repository.AdminSessionRepository, log *slog.Logger) (*Bot, error) {
	b, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("telegram: new bot: %w", err)
	}

	bot := &Bot{
		b:              b,
		adminID:        adminID,
		log:            log.With("component", "telegram"),
		userUC:      userUC,
		sessionRepo: sessionRepo,
	}
	bot.registerHandlers()
	return bot, nil
}

// InjectUseCases sets the support and broadcast use cases after construction.
// Required because Bot implements port.Sender which those use cases depend on,
// so they can only be built after the bot exists.
func (bot *Bot) InjectUseCases(supportUC *usecase.SupportUseCase, broadcastUC *usecase.BroadcastUseCase) {
	bot.supportUC = supportUC
	bot.broadcastUC = broadcastUC
}

// Start begins long-polling (blocks until Stop is called).
func (bot *Bot) Start() { bot.b.Start() }

// Stop gracefully stops the poller.
func (bot *Bot) Stop() { bot.b.Stop() }

// ── port.Sender implementation ─────────────────────────────────────────────────

// SendText sends a plain Markdown message to a chat.
func (bot *Bot) SendText(_ context.Context, telegramID int64, text string) error {
	_, err := bot.b.Send(chatByID(telegramID), text, tele.ModeMarkdown)
	return err
}

// SendPhoto sends a photo with a Markdown caption to a chat.
func (bot *Bot) SendPhoto(_ context.Context, telegramID int64, fileID, caption string) error {
	_, err := bot.b.Send(chatByID(telegramID), &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}, tele.ModeMarkdown)
	return err
}

// ── Handler registration ──────────────────────────────────────────────────────
func (bot *Bot) registerHandlers() {
	// Global middleware: panic recovery.
	bot.b.Use(middleware.Recover(func(err error) {
		bot.log.Error("handler panic", "err", err)
	}))

	// User commands.
	bot.b.Handle("/start", bot.handleStart)
	bot.b.Handle("/status", bot.handleStatus)
	bot.b.Handle("/support", bot.handleSupportPrompt)

	// Admin-only commands — guarded by adminOnly middleware at route level.
	adminGroup := bot.b.Group()
	adminGroup.Use(bot.adminOnly)
	adminGroup.Handle("/broadcast", bot.handleBroadcast)
	adminGroup.Handle("/bind", bot.handleBind)
	adminGroup.Handle("/sync", bot.handleSync)

	// Callback buttons.
	bot.b.Handle(&replyBtn, bot.handleReplyCallback)

	// Free-text and photo — routed differently for admin vs user.
	bot.b.Handle(tele.OnText, bot.routeText)
	bot.b.Handle(tele.OnPhoto, bot.routePhoto)
}

// adminOnly is a middleware that silently drops non-admin messages.
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
		bot.log.Error("start: register failed", "err", err, "user", c.Sender().ID)
		return c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}

	if u.IsLinked() {
		return c.Send("✅ Аккаунт привязан.\n\n/status — статус подписки\n/support — написать в поддержку")
	}
	return c.Send("👋 Ваш Telegram пока не привязан к аккаунту VPN.\n\nНапишите в /support — администратор поможет.")
}

func (bot *Bot) handleStatus(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

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
	return c.Send(text, tele.ModeMarkdown, tele.NoPreview)
}

func (bot *Bot) handleSupportPrompt(c tele.Context) error {
	return c.Send("📨 Напишите ваш вопрос следующим сообщением — ответим как можно скорее.")
}

// ── Message routing ───────────────────────────────────────────────────────────

var replyBtn = tele.InlineButton{Unique: "reply_to_user"}

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

	_, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Text(),
	})
	if err != nil {
		bot.log.Error("support: handle text", "err", err)
		return c.Send("⚠️ Не удалось доставить сообщение. Попробуйте позже.")
	}

	bot.notifyAdminAboutMessage(ctx, c.Sender().ID, c.Sender().Username, c.Text())
	return c.Send("✅ Сообщение получено — ответим скоро!")
}

func (bot *Bot) handleUserPhoto(c tele.Context) error {
	photo := c.Message().Photo
	if photo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	_, err := bot.supportUC.HandleUserMessage(ctx, usecase.IncomingMessage{
		TelegramID: c.Sender().ID,
		Username:   c.Sender().Username,
		Text:       c.Message().Caption,
		FileID:     photo.FileID,
	})
	if err != nil {
		bot.log.Error("support: handle photo", "err", err)
		return c.Send("⚠️ Не удалось доставить фото.")
	}

	fwdText := fmt.Sprintf("📸 *@%s* (`%d`) прислал фото.\n\n%s",
		c.Sender().Username, c.Sender().ID, c.Message().Caption)
	bot.notifyAdminWithPhoto(ctx, photo.FileID, c.Sender().ID, fwdText)
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

	session := repository.AdminSession{
		MessageID:  int(c.Message().ID),
		TargetTgID: targetID,
		ExpiresAt:  time.Now().Add(replyTTL),
	}
	if err := bot.sessionRepo.Save(ctx, session); err != nil {
		bot.log.Error("session save failed", "err", err)
	}

	_ = c.Respond(&tele.CallbackResponse{
		Text: fmt.Sprintf("Ответьте reply на это сообщение, чтобы написать пользователю %d", targetID),
	})
	_, _ = bot.b.Send(chatByID(bot.adminID),
		fmt.Sprintf("✏️ Ответьте reply на *это* сообщение для пользователя `%d`:", targetID),
		tele.ModeMarkdown)
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
		return nil // no pending reply for this message
	}
	if err != nil {
		bot.log.Error("session get failed", "err", err)
		return nil
	}

	// Consume the session — one reply per button click.
	if err := bot.sessionRepo.Delete(ctx, replyTo.ID); err != nil {
		bot.log.Warn("session delete failed", "err", err)
	}

	if _, err := bot.supportUC.HandleAdminReply(ctx, session.TargetTgID, c.Text(), ""); err != nil {
		bot.log.Error("support: admin reply failed", "err", err)
		return c.Send("⚠️ Не удалось отправить ответ.")
	}
	return c.Send(fmt.Sprintf("✅ Ответ отправлен пользователю `%d`.", session.TargetTgID), tele.ModeMarkdown)
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
		fmt.Sprintf("✅ Пользователь `%d` привязан к UUID `%s`.\n\nЕсли ещё не запускал бота — попросите нажать /start.", targetID, parts[2]),
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
		"✅ Синхронизация завершена\n\nВсего в панели: %d\nС telegram_id: %d\nСоздано: %d\nОбновлено: %d\nПропущено: %d",
		result.Total, result.Linked, result.Created, result.Updated, result.Skipped,
	))
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (bot *Bot) notifyAdminAboutMessage(ctx context.Context, senderID int64, username, text string) {
	btn := tele.InlineButton{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(senderID, 10),
	}
	markup := &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btn}}}
	fwdText := fmt.Sprintf("📩 *@%s* (`%d`)\n\n%s", username, senderID, text)

	if _, err := bot.b.Send(chatByID(bot.adminID), fwdText, tele.ModeMarkdown, markup); err != nil {
		bot.log.Warn("support: notify admin failed", "err", err)
	}
}

func (bot *Bot) notifyAdminWithPhoto(ctx context.Context, fileID string, senderID int64, caption string) {
	btn := tele.InlineButton{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(senderID, 10),
	}
	markup := &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btn}}}
	photo := &tele.Photo{File: tele.File{FileID: fileID}, Caption: caption}

	if _, err := bot.b.Send(chatByID(bot.adminID), photo, tele.ModeMarkdown, markup); err != nil {
		bot.log.Warn("support: notify admin (photo) failed", "err", err)
	}
}

func chatByID(id int64) *tele.Chat { return &tele.Chat{ID: id} }

// Compile-time check: *Bot must satisfy port.Sender.
var _ port.Sender = (*Bot)(nil)

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
