package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
)

func Status(sub *subscription.Status) string {
	statusLabel := "🟢 Активна"
	if !sub.IsActive || sub.IsExpired() {
		statusLabel = "🔴 Неактивна"
	}

	remaining := "∞"
	if sub.TotalTrafficBytes > 0 {
		remaining = FormatBytes(sub.RemainingTrafficBytes())
	}

	expire := "Бессрочно"
	if sub.ExpireDate != nil {
		expire = sub.ExpireDate.Format("02.01.2006")
	}

	return fmt.Sprintf(
		"📊 *Статус подписки*\n\n"+
			"Статус: %s\n"+
			"Использовано: %s\n"+
			"Остаток: %s\n"+
			"Истекает: %s\n\n"+
			"🔗 [Ссылка на подписку](%s)",
		statusLabel,
		FormatBytes(sub.UsedTrafficBytes),
		remaining,
		expire,
		sub.SubscriptionURL,
	)
}

func LocalUsers(title string, users []*user.User) string {
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
			i+1, name, u.TelegramID, canMsg, ShortID(u.HiddifyUUID))
	}
	sb.WriteString("\nmsg: да — бот может писать; msg: нет — не запускал, заблокировал или доставка падала.")
	return sb.String()
}

func PanelUsers(title string, users []service.PanelUserView) string {
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
			i+1, name, tg, botState, ShortID(u.UUID))
	}
	sb.WriteString("\nbot: пишет — бот может отправлять; не пишет — заблокирован/доставка падала; не запускал — tg есть в панели, но /start не было.")
	return sb.String()
}

func UnboundPanelUsers(users []subscription.PanelUser) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔎 Hiddify без telegram_id: %d\n\n", len(users))

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
		fmt.Fprintf(&sb, "%d. %s | uuid: %s\n", i+1, name, ShortID(u.UUID))
	}
	sb.WriteString("\nДля привязки: /bind <telegram_id> <uuid>")
	return sb.String()
}

func History(telegramID int64, msgs []*ticket.Message) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 *История для* `%d`\n\n", telegramID)

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
	return sb.String()
}

func ShortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func FormatBytes(b int64) string {
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
