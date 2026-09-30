package tg

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/service"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/transport/tg/markup"
)

const (
	startPayloadPrefix = "i_"
	codeWaitTTL        = 10 * time.Minute
	maxInvitesShown    = 30
)

// --- «waiting for a code» state ---------------------------------------------

// codeWaiters remembers who pressed «У меня есть код»: the next text from such
// a person is a code, not a support message. In memory, expires by itself.
type codeWaiters struct {
	mu    sync.Mutex
	until map[int64]time.Time
	now   func() time.Time
}

func newCodeWaiters() *codeWaiters {
	return &codeWaiters{until: make(map[int64]time.Time), now: time.Now}
}

func (w *codeWaiters) arm(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.until[id] = w.now().Add(codeWaitTTL)
}

func (w *codeWaiters) clear(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.until, id)
}

// take reports whether id was waiting (and not expired) and clears the state.
func (w *codeWaiters) take(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	until, ok := w.until[id]
	delete(w.until, id)
	return ok && w.now().Before(until)
}

// --- admin: /invite ---------------------------------------------------------

func (bot *Bot) handleInvite(c tele.Context) error {
	return c.Send("🎟 Новый код приглашения. Кому?", markup.InviteKind())
}

// handleInviteCallback serves inv_* buttons. The admin check is explicit:
// callbacks are not behind adminOnly.
func (bot *Bot) handleInviteCallback(c tele.Context) error {
	if c.Sender().ID != bot.adminID {
		return nil
	}
	data := c.Data()

	switch {
	case strings.HasPrefix(data, "inv_kind:"):
		kind := invite.Kind(strings.TrimPrefix(data, "inv_kind:"))
		if !kind.Valid() {
			return nil
		}
		return bot.editOrSend(c, fmt.Sprintf("🎟 Кому: %s. На какой срок?", kind.Label()), markup.InviteDays(kind))

	case strings.HasPrefix(data, "inv_days:"):
		parts := strings.Split(strings.TrimPrefix(data, "inv_days:"), ":")
		if len(parts) != 2 {
			return nil
		}
		days, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		return bot.issueInvite(c, invite.Kind(parts[0]), days)

	case strings.HasPrefix(data, "inv_revoke:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(data, "inv_revoke:"), 10, 64)
		if err != nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
		defer cancel()
		switch err := bot.inviteUC.Revoke(ctx, id); {
		case err == nil, errors.Is(err, domain.ErrNotFound): // already gone: the refreshed list shows it
		default:
			bot.log.Error("invite revoke failed", "invite_id", id, "err", err)
			return c.Send("⚠️ Не удалось отозвать код.")
		}
		return bot.showInvites(c, true)
	}
	return nil
}

func (bot *Bot) issueInvite(c tele.Context, kind invite.Kind, days int) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	issued, err := bot.inviteUC.Issue(ctx, kind, days)
	if err != nil {
		bot.log.Error("invite issue failed", "err", err)
		return c.Send("⚠️ Не удалось создать код.")
	}

	// Drop the buttons of the wizard message, then send the code itself.
	_ = bot.editOrSend(c, fmt.Sprintf("✅ Код создан: %s, %s.", kind.Label(), invite.DaysLabel(days)), nil)

	t := buildInviteTexts(bot.b.Me.Username, issued.Code)
	text := fmt.Sprintf("🎟 Код: %s\nКому: %s, срок: %s. Действует 7 дней.\n\nТекст для пересылки:\n\n%s",
		issued.Code, kind.Label(), invite.DaysLabel(days), t.forward)
	return c.Send(text, tele.NoPreview, markup.InviteShare(t.shareURL))
}

// inviteTexts are the ready-to-forward pieces for one code.
type inviteTexts struct {
	link     string // t.me deep link
	forward  string // text for the person
	shareURL string // t.me/share/url that opens the chat picker
}

// buildInviteTexts makes the forwardable text and the share URL. code is the
// display form XXXXX-XXXXX; the deep-link payload uses it without the hyphen.
func buildInviteTexts(botUsername, code string) inviteTexts {
	link := fmt.Sprintf("https://t.me/%s?start=%s%s", botUsername, startPayloadPrefix, strings.ReplaceAll(code, "-", ""))
	forward := fmt.Sprintf("Откройте ссылку и нажмите «Старт»: %s. Если ссылка не открывается — откройте бота @%s, нажмите «🎟 У меня есть код» и отправьте код: %s",
		link, botUsername, code)
	short := fmt.Sprintf("Если ссылка не открывается — откройте бота @%s, нажмите «🎟 У меня есть код» и отправьте код: %s", botUsername, code)
	return inviteTexts{
		link:     link,
		forward:  forward,
		shareURL: "https://t.me/share/url?url=" + escapeQuery(link) + "&text=" + escapeQuery(short),
	}
}

// escapeQuery percent-encodes a query value with %20 for spaces (not "+"),
// which every Telegram client reads the same way.
func escapeQuery(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// --- admin: /invites ----------------------------------------------------------

func (bot *Bot) handleInvites(c tele.Context) error { return bot.showInvites(c, false) }

func (bot *Bot) showInvites(c tele.Context, edit bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	list, err := bot.inviteUC.ListActive(ctx)
	if err != nil {
		bot.log.Error("invite list failed", "err", err)
		return c.Send("⚠️ Не удалось получить список кодов.")
	}
	text, ids := formatInviteList(list)
	var m *tele.ReplyMarkup
	if len(ids) > 0 {
		m = markup.InviteRevokeList(ids)
	}
	if edit {
		return bot.editOrSend(c, text, m)
	}
	if m == nil {
		return c.Send(text)
	}
	return c.Send(text, m)
}

// formatInviteList renders active codes without the codes themselves (only
// hashes are stored) and returns the ids shown, for the revoke buttons.
func formatInviteList(list []*invite.Invite) (string, []int64) {
	if len(list) == 0 {
		return "Активных кодов нет.", nil
	}
	shown := list
	if len(shown) > maxInvitesShown {
		shown = shown[:maxInvitesShown]
	}
	var b strings.Builder
	b.WriteString("🎟 Активные коды:\n")
	ids := make([]int64, 0, len(shown))
	for _, inv := range shown {
		ids = append(ids, inv.ID)
		fmt.Fprintf(&b, "\n#%d · %s · %s · до %s", inv.ID, inv.Kind.Label(), invite.DaysLabel(inv.Days),
			inv.ExpiresAt.Local().Format("02.01 15:04"))
		if inv.ClaimedAt != nil {
			b.WriteString(" · ожидает завершения")
		}
	}
	if len(list) > len(shown) {
		fmt.Fprintf(&b, "\n\nПоказаны первые %d из %d.", len(shown), len(list))
	}
	return b.String(), ids
}

// editOrSend edits the callback's message, falling back to a new message.
// A nil markup removes the buttons (editing without reply_markup does that).
func (bot *Bot) editOrSend(c tele.Context, text string, m *tele.ReplyMarkup) error {
	var opts []interface{}
	if m != nil {
		opts = append(opts, m)
	}
	if c.Message() != nil {
		if _, err := bot.b.Edit(c.Message(), text, opts...); err == nil {
			return nil
		}
	}
	return c.Send(text, opts...)
}

// --- users: redeeming a code --------------------------------------------------

// redeemAndReply runs the redemption and answers the person. alreadySubscribed
// is true if they already have a subscription (the caller may show the status).
// rearm keeps the «waiting for code» state after a wrong code.
func (bot *Bot) redeemAndReply(c tele.Context, raw string, rearm bool) (alreadySubscribed bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	sender := c.Sender()
	res, err := bot.inviteUC.Redeem(ctx, sender.ID, sender.Username, raw)
	switch {
	case err == nil:
		bot.announceRedeemed(sender, res)
		return false, c.Send(
			"🎉 Готово! Подписка создана.\n\nВаша ссылка для приложения:\n"+res.SubscriptionURL+
				"\n\nДобавьте её в приложение Hiddify. Статус подписки — /status.",
			tele.NoPreview, markup.StatusMenu(),
		)
	case errors.Is(err, service.ErrInviteInvalid):
		if rearm {
			bot.codeWait.arm(sender.ID)
		}
		return false, c.Send("❌ Код недействителен. Проверьте, что он введён без ошибок, или попросите новый у администратора.", markup.UnlinkedMenu())
	case errors.Is(err, service.ErrInviteRateLimited):
		return false, c.Send("⏳ Слишком много неверных попыток. Попробуйте через 10 минут.")
	case errors.Is(err, service.ErrAlreadySubscribed):
		return true, c.Send("ℹ️ У вас уже есть подписка.")
	case errors.Is(err, service.ErrInviteRetryLater):
		bot.codeWait.arm(sender.ID)
		return false, c.Send("⏳ Не получилось с первого раза. Попробуйте ещё раз через минуту: отправьте тот же код или нажмите «Старт» по ссылке ещё раз.")
	case errors.Is(err, service.ErrInviteRejected):
		return false, c.Send("⚠️ Не удалось создать подписку. Напишите в поддержку (/support), мы разберёмся.")
	default:
		bot.log.Error("redeem failed", "err", err)
		return false, c.Send("⚠️ Произошла ошибка. Попробуйте позже.")
	}
}

// announceRedeemed tells the admin who redeemed a code and that Apply is needed.
// The bot never calls Apply itself.
func (bot *Bot) announceRedeemed(sender *tele.User, res *service.RedeemResult) {
	who := "без username"
	if sender.Username != "" {
		who = "@" + sender.Username
	}
	msg := fmt.Sprintf("🎟 Код #%d погашен: %s (id %d)\nТариф: %s, срок: %s.\n\n"+
		"⚠️ Нужен Apply в панели: новый пользователь сначала работает только на запасном профиле.",
		res.InviteID, who, sender.ID, res.Kind.Label(), invite.DaysLabel(res.Days))
	if _, err := bot.b.Send(chatByID(bot.adminID), msg); err != nil {
		bot.log.Warn("invite: notify admin failed", "invite_id", res.InviteID, "err", err)
	}
}

// tryHandleCodeText treats a user's text as an invite code only if they
// pressed «У меня есть код» (waiting state). Everything else goes on to the
// support routing untouched.
func (bot *Bot) tryHandleCodeText(c tele.Context) (handled bool, err error) {
	text := c.Text()
	if strings.HasPrefix(text, "/") {
		return false, nil
	}
	if !bot.codeWait.take(c.Sender().ID) {
		return false, nil
	}
	_, err = bot.redeemAndReply(c, text, true)
	return true, err
}

func (bot *Bot) handleHaveCode(c tele.Context) error {
	bot.codeWait.arm(c.Sender().ID)
	return c.Send("🎟 Отправьте код одним сообщением (формат XXXXX-XXXXX).")
}
