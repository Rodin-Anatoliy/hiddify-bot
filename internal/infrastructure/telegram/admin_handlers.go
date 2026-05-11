package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

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
	text, err := bot.usersText(ctx, mode)
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

func (bot *Bot) usersText(ctx context.Context, mode string) (string, error) {
	switch mode {
	case "all":
		return bot.usersAllText(ctx)
	case "unbound":
		return bot.usersUnboundText(ctx)
	case "blocked":
		return bot.usersBlockedText(ctx)
	default:
		return "Использование:\n/users\n/users unbound\n/users blocked", nil
	}
}

func (bot *Bot) usersAllText(ctx context.Context) (string, error) {
	users, err := bot.userUC.ListPanelUserViews(ctx)
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "В Hiddify нет пользователей.", nil
	}
	return formatPanelUsers("👥 Пользователи Hiddify", users), nil
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

// handleApproveAccess processes admin approval of an access request.
// Data format: "approve:<telegram_id>:<username>"
func (bot *Bot) handleApproveAccess(c tele.Context) error {
	_ = c.Respond()

	targetID, err := strconv.ParseInt(strings.TrimPrefix(c.Data(), "approve:"), 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id в заявке.")
	}

	// Fetch username from local DB if user already pressed /start, otherwise empty.
	var username string
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()
	if existing, dbErr := bot.userUC.GetUser(ctx, targetID); dbErr == nil {
		username = existing.Username
	}

	created, err := bot.userUC.ApproveAccessRequest(ctx, targetID, username)
	if err != nil {
		bot.log.Error("approve access: failed", "err", err, "target", targetID)
		return c.Send("⚠️ Ошибка при создании аккаунта: " + err.Error())
	}

	// Edit admin's message to remove buttons and show result.
	_, _ = bot.b.Edit(
		c.Message(),
		fmt.Sprintf("✅ Одобрено. Аккаунт создан.\nUUID: `%s`\nTelegram ID: `%d`", created.UUID, targetID),
		tele.ModeMarkdown,
	)

	// Notify the user.
	userMsg := fmt.Sprintf(
		"🎉 *Доступ одобрен!*\n\n"+
			"Ваш аккаунт создан. Нажмите /start чтобы увидеть статус подписки.\n\n"+
			"🔗 [Ссылка на подписку](%s)",
		created.SubscriptionURL,
	)
	if _, err := bot.b.Send(chatByID(targetID), userMsg, tele.ModeMarkdown, tele.NoPreview); err != nil {
		bot.log.Warn("approve access: notify user failed", "err", err, "target", targetID)
		return c.Send(fmt.Sprintf("⚠️ Аккаунт создан, но уведомить пользователя не удалось. UUID: `%s`", created.UUID), tele.ModeMarkdown)
	}
	return nil
}

// handleRejectAccess processes admin rejection of an access request.
// Data format: "reject:<telegram_id>"
func (bot *Bot) handleRejectAccess(c tele.Context) error {
	_ = c.Respond()

	targetID, err := strconv.ParseInt(strings.TrimPrefix(c.Data(), "reject:"), 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id.")
	}

	// Edit admin message to remove buttons.
	_, _ = bot.b.Edit(
		c.Message(),
		fmt.Sprintf("❌ Заявка от `%d` отклонена.", targetID),
		tele.ModeMarkdown,
	)

	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	if _, err := bot.b.Send(
		chatByID(targetID),
		"❌ К сожалению, ваша заявка на подключение отклонена.\n\nЕсли считаете это ошибкой — напишите в поддержку.",
		tele.ModeMarkdown,
	); err != nil {
		bot.log.Warn("reject access: notify user failed", "err", err, "target", targetID)
	}
	_ = ctx
	return nil
}
