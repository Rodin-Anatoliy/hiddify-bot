package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

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
