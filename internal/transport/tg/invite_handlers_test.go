package tg

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
)

func TestBuildInviteTexts(t *testing.T) {
	got := buildInviteTexts("example_bot", "K7QM4-XTP9A")

	if got.link != "https://t.me/example_bot?start=i_K7QM4XTP9A" {
		t.Fatalf("link = %q", got.link)
	}
	payload := strings.TrimPrefix(got.link, "https://t.me/example_bot?start=")
	if len(payload) > 64 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(payload) {
		t.Fatalf("payload %q violates Telegram start-parameter rules", payload)
	}

	want := "Откройте ссылку и нажмите «Старт»: https://t.me/example_bot?start=i_K7QM4XTP9A. " +
		"Если ссылка не открывается — откройте бота @example_bot, нажмите «🎟 У меня есть код» и отправьте код: K7QM4-XTP9A"
	if got.forward != want {
		t.Fatalf("forward text = %q", got.forward)
	}
	// Only t.me links: no web portal link.
	if strings.Count(got.forward, "http") != 1 {
		t.Fatalf("forward text must contain only the t.me link: %q", got.forward)
	}

	if strings.Contains(got.shareURL, "+") {
		t.Fatalf("share url must encode spaces as %%20: %q", got.shareURL)
	}
	u, err := url.Parse(got.shareURL)
	if err != nil {
		t.Fatalf("share url: %v", err)
	}
	if u.Scheme != "https" || u.Host != "t.me" || u.Path != "/share/url" {
		t.Fatalf("share url = %q", got.shareURL)
	}
	if u.Query().Get("url") != got.link {
		t.Fatalf("share url param = %q, want the start link", u.Query().Get("url"))
	}
	if !strings.Contains(u.Query().Get("text"), "K7QM4-XTP9A") {
		t.Fatalf("share text = %q", u.Query().Get("text"))
	}
}

func TestFormatInviteListHasNoCodesAndOneIDPerInvite(t *testing.T) {
	exp := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	claimed := exp
	text, ids := formatInviteList([]*invite.Invite{
		{ID: 3, Kind: invite.KindOwn, Days: invite.Unlimited, ExpiresAt: exp},
		{ID: 5, Kind: invite.KindFriend, Days: 30, ExpiresAt: exp, ClaimedAt: &claimed},
	})
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 5 {
		t.Fatalf("ids = %v", ids)
	}
	for _, want := range []string{"#3", "Свой", "бессрочно", "#5", "Знакомый", "месяц", "ожидает завершения"} {
		if !strings.Contains(text, want) {
			t.Errorf("list text lacks %q:\n%s", want, text)
		}
	}

	empty, none := formatInviteList(nil)
	if len(none) != 0 || empty == "" {
		t.Fatalf("empty list = %q, %v", empty, none)
	}
}

func TestFormatInviteListCapsLength(t *testing.T) {
	var list []*invite.Invite
	for i := int64(1); i <= maxInvitesShown+5; i++ {
		list = append(list, &invite.Invite{ID: i, Kind: invite.KindOwn, Days: 7, ExpiresAt: time.Now()})
	}
	_, ids := formatInviteList(list)
	if len(ids) != maxInvitesShown {
		t.Fatalf("shown = %d, want %d", len(ids), maxInvitesShown)
	}
}

func TestCodeWaiters(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w := newCodeWaiters()
	w.now = func() time.Time { return now }

	if w.take(1) {
		t.Fatal("nobody is waiting yet")
	}
	w.arm(1)
	if w.take(2) {
		t.Fatal("another person must not consume the state")
	}
	if !w.take(1) {
		t.Fatal("armed person must be waiting")
	}
	if w.take(1) {
		t.Fatal("state is one-shot")
	}

	w.arm(1)
	now = now.Add(codeWaitTTL + time.Second)
	if w.take(1) {
		t.Fatal("state must expire")
	}

	w.arm(1)
	w.clear(1)
	if w.take(1) {
		t.Fatal("cleared state must be gone")
	}
}
