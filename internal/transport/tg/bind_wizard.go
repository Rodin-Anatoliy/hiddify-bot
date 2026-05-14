package tg

// bindWizard implements a step-by-step /bind flow.
// Step 1: /bind → bot asks for Telegram ID
// Step 2: admin sends TG ID → bot asks for Hiddify UUID
// Step 3: admin sends UUID → bot links and confirms
//
// Wizard state is persisted in admin_sessions with reserved negative message IDs,
// so the wizard survives bot restarts.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/admin"
)

const (
	bindWizardStepTgID = -10 // session ID meaning: waiting for telegram_id input
	bindWizardStepUUID = -11 // session ID meaning: waiting for uuid input
)

// handleBind starts the bind wizard or falls back to inline mode.
func (bot *Bot) handleBind(c tele.Context) error {
	parts := strings.Fields(c.Text())

	// Legacy inline mode: /bind <tg_id> <uuid> — still supported.
	if len(parts) == 3 {
		return bot.bindInline(c, parts[1], parts[2])
	}

	// Wizard mode: /bind alone starts the dialog.
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	_ = bot.sessionRepo.Save(ctx, admin.Session{
		MessageID: bindWizardStepTgID,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})

	return c.Send(
		"🔗 *Привязка пользователя*\n\nШаг 1/2: введите Telegram ID пользователя (число):",
		tele.ModeMarkdown,
	)
}

// bindInline handles the legacy /bind <tg_id> <uuid> format.
func (bot *Bot) bindInline(c tele.Context, rawID, uuid string) error {
	targetID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return c.Send("⚠️ Неверный telegram_id — должно быть число.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()
	return bot.executeBind(ctx, c, targetID, uuid)
}

// tryHandleBindWizard checks if admin is mid-wizard and handles their input.
// Returns true if the message was consumed by the wizard.
func (bot *Bot) tryHandleBindWizard(c tele.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	// Step 1: waiting for TG ID.
	if sess, err := bot.sessionRepo.Get(ctx, bindWizardStepTgID); err == nil && sess != nil {
		targetID, parseErr := strconv.ParseInt(strings.TrimSpace(c.Text()), 10, 64)
		if parseErr != nil {
			return true, c.Send("⚠️ Введите корректный Telegram ID (только цифры):")
		}

		_ = bot.sessionRepo.Delete(ctx, bindWizardStepTgID)
		_ = bot.sessionRepo.Save(ctx, admin.Session{
			MessageID:  bindWizardStepUUID,
			TargetTgID: targetID,
			ExpiresAt:  time.Now().Add(10 * time.Minute),
		})

		return true, c.Send(
			fmt.Sprintf("✅ TG ID: `%d`\n\nШаг 2/2: введите Hiddify UUID:", targetID),
			tele.ModeMarkdown,
		)
	}

	// Step 2: waiting for UUID.
	if sess, err := bot.sessionRepo.Get(ctx, bindWizardStepUUID); err == nil && sess != nil {
		uuid := strings.TrimSpace(c.Text())
		targetID := sess.TargetTgID
		_ = bot.sessionRepo.Delete(ctx, bindWizardStepUUID)
		return true, bot.executeBind(ctx, c, targetID, uuid)
	}

	return false, nil
}

// executeBind performs the actual linking and reports result.
func (bot *Bot) executeBind(ctx context.Context, c tele.Context, targetID int64, uuid string) error {
	if err := bot.userUC.LinkManually(ctx, targetID, uuid); err != nil {
		return c.Send("⚠️ Ошибка привязки: " + err.Error())
	}
	return c.Send(
		fmt.Sprintf(
			"✅ Пользователь `%d` привязан к UUID `%s`.\n\nЕсли не запускал бота — попросите нажать /start.",
			targetID, uuid,
		),
		tele.ModeMarkdown,
	)
}
