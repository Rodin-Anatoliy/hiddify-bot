package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

type Bot struct {
	b       *tele.Bot
	adminID int64
	log     *slog.Logger

	userUC      *usecase.UserUseCase
	supportUC   *usecase.SupportUseCase
	broadcastUC *usecase.BroadcastUseCase

	pendingReplies map[int]int64
}

func New(
	token string,
	adminID int64,
	userUC *usecase.UserUseCase,
	supportUC *usecase.SupportUseCase,
	broadcastUC *usecase.BroadcastUseCase,
	log *slog.Logger,
) (*Bot, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("telegram: init bot: %w", err)
	}

	bot := &Bot{
		b:              b,
		adminID:        adminID,
		log:            log.With("component", "telegram"),
		userUC:         userUC,
		supportUC:      supportUC,
		broadcastUC:    broadcastUC,
		pendingReplies: make(map[int]int64),
	}
	bot.registerHandlers()
	return bot, nil
}

func (bot *Bot) Start() { bot.b.Start() }

func (bot *Bot) Stop() { bot.b.Stop() }

func (bot *Bot) SendText(_ context.Context, telegramID int64, text string) error {
	_, err := bot.b.Send(&tele.Chat{ID: telegramID}, text, tele.ModeMarkdown)
	return err
}

func (bot *Bot) SendPhoto(_ context.Context, telegramID int64, fileID, caption string) error {
	photo := &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}
	_, err := bot.b.Send(&tele.Chat{ID: telegramID}, photo, tele.ModeMarkdown)
	return err
}

func (bot *Bot) registerHandlers() {
	bot.b.Use(func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			defer func() {
				if r := recover(); r != nil {
					bot.log.Error("handler panic", "recover", r)
				}
			}()
			return next(c)
		}
	})

	bot.b.Handle("/start", bot.handleStart)
	bot.b.Handle("/status", bot.handleStatus)
	bot.b.Handle("/link", bot.handleLink)
	bot.b.Handle("/support", bot.handleSupportInfo)

	bot.b.Handle("/broadcast", bot.handleBroadcast)
	bot.b.Handle("/bind", bot.handleBind)
	bot.b.Handle("/sync", bot.handleSync)

	bot.b.Handle(&replyBtn, bot.handleReplyCallback)

	bot.b.Handle(tele.OnText, bot.handleIncomingMessage)
	bot.b.Handle(tele.OnPhoto, bot.handleIncomingPhoto)
}

func (bot *Bot) handleStart(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	u, err := bot.userUC.RegisterOrGet(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		bot.log.Error("start: register", "err", err)
		return c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}

	if u.IsLinked() {
		return c.Send("Аккаунт привязан.\n\n/status — статус подписки\n/support — связь с администратором")
	}
	return c.Send("Telegram пока не привязан к аккаунту VPN.\n\nНапишите в /support и укажите имя или email.")
}

func (bot *Bot) handleStatus(c tele.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sub, err := bot.userUC.GetSubscription(ctx, c.Sender().ID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return c.Send("❌ Ваш аккаунт не найден. Используйте /start.")
		}
		return c.Send("⚠️ Не удалось получить статус. Попробуйте позже.")
	}

	status := "🟢 Активна"
	if !sub.IsActive || sub.IsExpired() {
		status = "🔴 Неактивна"
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
		status,
		formatBytes(sub.UsedTrafficBytes),
		remaining,
		expire,
		sub.SubscriptionURL,
	)
	return c.Send(text, tele.ModeMarkdown, tele.NoPreview)
}

func (bot *Bot) handleLink(c tele.Context) error {
	return c.Send("ℹ️ Для привязки аккаунта обратитесь к администратору. " +
		"Напишите в /support и укажите ваш email или имя.")
}

func (bot *Bot) handleSupportInfo(c tele.Context) error {
	return c.Send("📨 Напишите ваш вопрос следующим сообщением, и мы ответим как можно скорее.")
}

var replyBtn = tele.InlineButton{Unique: "reply_to_user"}

func (bot *Bot) handleIncomingMessage(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		return bot.handleAdminTextReply(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := bot.supportUC.HandleUserMessage(ctx,
		c.Sender().ID, c.Sender().Username, c.Text(), "")
	if err != nil {
		bot.log.Error("support: handle message", "err", err)
		return c.Send("⚠️ Не удалось доставить сообщение. Попробуйте позже.")
	}

	btn := tele.InlineButton{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(c.Sender().ID, 10),
	}
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{{btn}}

	if err := bot.sendToAdmin(ctx,
		fmt.Sprintf("📩 *@%s* (ID: `%d`)\n\n%s", c.Sender().Username, c.Sender().ID, c.Text()),
		nil, markup); err != nil {
		bot.log.Warn("support: notify admin", "err", err)
	}

	return c.Send("✅ Ваше сообщение получено. Ответим скоро!")
}

func (bot *Bot) handleIncomingPhoto(c tele.Context) error {
	if c.Sender().ID == bot.adminID {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	photo := c.Message().Photo
	if photo == nil {
		return nil
	}
	caption := c.Message().Caption
	_, err := bot.supportUC.HandleUserMessage(ctx,
		c.Sender().ID, c.Sender().Username, caption, photo.FileID)
	if err != nil {
		return c.Send("⚠️ Не удалось доставить фото.")
	}

	btn := tele.InlineButton{
		Unique: "reply_to_user",
		Text:   "↩️ Ответить",
		Data:   strconv.FormatInt(c.Sender().ID, 10),
	}
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{{btn}}

	fwdText := fmt.Sprintf("📸 *@%s* (ID: `%d`) прислал фото.\n\n%s",
		c.Sender().Username, c.Sender().ID, caption)
	_ = bot.sendPhotoToAdmin(ctx, photo.FileID, fwdText, markup)

	return c.Send("✅ Фото получено!")
}

func (bot *Bot) handleReplyCallback(c tele.Context) error {
	targetID, err := strconv.ParseInt(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Invalid user ID"})
	}

	bot.pendingReplies[int(c.Message().ID)] = targetID

	// Auto-cleanup this entry after 30 minutes
	messageID := int(c.Message().ID)
	time.AfterFunc(30*time.Minute, func() {
		delete(bot.pendingReplies, messageID)
	})

	_ = c.Respond(&tele.CallbackResponse{
		Text: fmt.Sprintf("Теперь ответьте на это сообщение (reply), чтобы написать пользователю %d", targetID),
	})

	_, _ = bot.b.Send(
		&tele.Chat{ID: bot.adminID},
		fmt.Sprintf("✏️ Введите ответ для пользователя `%d`. "+
			"Используйте reply на это сообщение:", targetID),
		tele.ModeMarkdown,
	)
	return nil
}

func (bot *Bot) handleAdminTextReply(c tele.Context) error {
	replyTo := c.Message().ReplyTo
	if replyTo == nil {
		return nil
	}

	targetID, ok := bot.pendingReplies[replyTo.ID]
	if !ok {
		return nil
	}
	delete(bot.pendingReplies, replyTo.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := bot.supportUC.HandleAdminReply(ctx, targetID, c.Text(), ""); err != nil {
		bot.log.Error("support: admin reply", "err", err)
		return c.Send("⚠️ Не удалось отправить ответ пользователю.")
	}
	return c.Send(fmt.Sprintf("✅ Ответ отправлен пользователю `%d`.", targetID), tele.ModeMarkdown)
}

func (bot *Bot) handleBroadcast(c tele.Context) error {
	if c.Sender().ID != bot.adminID {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var msg usecase.BroadcastMessage
	if c.Message().Photo != nil {
		msg.FileID = c.Message().Photo.FileID
		msg.Text = c.Message().Caption
	} else {
		msg.Text = strings.TrimPrefix(c.Text(), "/broadcast ")
		if msg.Text == "/broadcast" || msg.Text == "" {
			return c.Send("Использование: /broadcast <текст>\nИли прикрепите фото с подписью.")
		}
	}

	_ = c.Send("📤 Рассылка запущена...")

	result, err := bot.broadcastUC.Send(ctx, msg)
	if err != nil {
		return c.Send("⚠️ Ошибка при рассылке: " + err.Error())
	}
	return c.Send(fmt.Sprintf(
		"✅ Рассылка завершена\n\nВсего: %d\n✔️ Доставлено: %d\n❌ Ошибок: %d",
		result.Total, result.Success, result.Failed,
	))
}

func (bot *Bot) handleBind(c tele.Context) error {
	if c.Sender().ID != bot.adminID {
		return nil
	}
	parts := strings.Fields(c.Text())
	if len(parts) != 3 {
		return c.Send("Использование: /bind <telegram_id> <hiddify_uuid>")
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id")
	}
	uuid := parts[2]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := bot.userUC.LinkManually(ctx, targetID, uuid); err != nil {
		return c.Send("⚠️ Ошибка привязки: " + err.Error())
	}
	return c.Send(fmt.Sprintf("✅ Пользователь `%d` привязан к UUID `%s`.\n\nЕсли он еще не запускал бота, попросите нажать /start.", targetID, uuid), tele.ModeMarkdown)
}

func (bot *Bot) handleSync(c tele.Context) error {
	if c.Sender().ID != bot.adminID {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := bot.userUC.SyncFromHiddify(ctx)
	if err != nil {
		return c.Send("⚠️ Ошибка синхронизации: " + err.Error())
	}
	return c.Send(fmt.Sprintf(
		"✅ Синхронизация завершена\n\nВсего в панели: %d\nС telegram_id: %d\nСоздано: %d\nОбновлено: %d\nПропущено: %d",
		result.Total,
		result.Linked,
		result.Created,
		result.Updated,
		result.Skipped,
	))
}

func (bot *Bot) sendToAdmin(ctx context.Context, text string, _ any, markup *tele.ReplyMarkup) error {
	opts := []any{tele.ModeMarkdown}
	if markup != nil {
		opts = append(opts, markup)
	}
	_, err := bot.b.Send(&tele.Chat{ID: bot.adminID}, text, opts...)
	return err
}

func (bot *Bot) sendPhotoToAdmin(_ context.Context, fileID, caption string, markup *tele.ReplyMarkup) error {
	photo := &tele.Photo{
		File:    tele.File{FileID: fileID},
		Caption: caption,
	}
	opts := []any{tele.ModeMarkdown}
	if markup != nil {
		opts = append(opts, markup)
	}
	_, err := bot.b.Send(&tele.Chat{ID: bot.adminID}, photo, opts...)
	return err
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
