package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/sqlite"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

// --- fakes -----------------------------------------------------------------

// fakePanel is an in-memory panel. All state is behind mu (CI runs with -race).
type fakePanel struct {
	mu           sync.Mutex
	byTelegram   map[int64]string
	createCalls  int
	lookupCalls  int
	createReqs   []subscription.CreateUserRequest
	createErr    error         // returned from CreateUser
	createAnyway bool          // with createErr: the user is created although an error is returned
	lookupErr    error         // returned from GetUserByTelegramID
	createDelay  time.Duration // simulates the slow panel
}

func newFakePanel() *fakePanel { return &fakePanel{byTelegram: map[int64]string{}} }

func (p *fakePanel) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.createCalls
}

func (p *fakePanel) lookups() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lookupCalls
}

func (p *fakePanel) GetUserByUUID(context.Context, string) (*subscription.Status, error) {
	return nil, domain.ErrNotFound
}

func (p *fakePanel) GetUserByTelegramID(_ context.Context, tg int64) (*subscription.Status, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lookupCalls++
	if p.lookupErr != nil {
		return nil, "", p.lookupErr
	}
	uuid, ok := p.byTelegram[tg]
	if !ok {
		return nil, "", domain.ErrNotFound
	}
	return &subscription.Status{UUID: uuid, SubscriptionURL: "https://panel.example/sub/" + uuid + "/"}, uuid, nil
}

func (p *fakePanel) ListStatusesByTelegramID(context.Context, int64) ([]*subscription.Status, error) {
	return nil, domain.ErrNotFound
}
func (p *fakePanel) ListPanelUsers(context.Context) ([]subscription.PanelUser, error) {
	return nil, nil
}
func (p *fakePanel) SetTelegramID(context.Context, string, int64) error { return nil }

func (p *fakePanel) CreateUser(_ context.Context, req subscription.CreateUserRequest) (*subscription.CreatedUser, error) {
	if p.createDelay > 0 {
		time.Sleep(p.createDelay)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createCalls++
	p.createReqs = append(p.createReqs, req)
	if p.createErr != nil && !p.createAnyway {
		return nil, p.createErr
	}
	uuid := fmt.Sprintf("fake-uuid-%d", p.createCalls)
	p.byTelegram[req.TelegramID] = uuid
	if p.createErr != nil {
		return nil, p.createErr
	}
	return &subscription.CreatedUser{UUID: uuid, SubscriptionURL: "https://panel.example/sub/" + uuid + "/"}, nil
}

func (p *fakePanel) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createErr, p.createAnyway = err, false
}

func (p *fakePanel) heal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createErr, p.createAnyway = nil, false
}

// countingInvites counts how often the use case touches the invites table.
type countingInvites struct {
	invite.Repository
	mu sync.Mutex
	n  int
}

func (c *countingInvites) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func (c *countingInvites) hit() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *countingInvites) Claim(ctx context.Context, h string, tg int64, now time.Time) (*invite.Claim, error) {
	c.hit()
	return c.Repository.Claim(ctx, h, tg, now)
}

// --- fixture ---------------------------------------------------------------

type fixture struct {
	uc      *InviteUseCase
	panel   *fakePanel
	users   user.Repository
	invites *countingInvites
	clock   *time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	f := &fixture{
		panel:   newFakePanel(),
		users:   sqlite.NewUserRepository(db),
		invites: &countingInvites{Repository: sqlite.NewInviteRepository(db)},
	}
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	f.clock = &start
	f.uc = NewInviteUseCase(f.invites, f.users, f.panel, logger.New("debug"))
	f.uc.now = func() time.Time { return *f.clock }
	return f
}

func (f *fixture) advance(d time.Duration) { *f.clock = f.clock.Add(d) }

func (f *fixture) issue(t *testing.T, kind invite.Kind, days int) *Issued {
	t.Helper()
	iss, err := f.uc.Issue(context.Background(), kind, days)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return iss
}

func (f *fixture) redeem(tg int64, code string) (*RedeemResult, error) {
	return f.uc.Redeem(context.Background(), tg, "alice", code)
}

func timeoutErr() error { return fmt.Errorf("hiddify create user: %w", context.DeadlineExceeded) }

func statusErr(code int) error {
	return fmt.Errorf("hiddify create user: %w", fmt.Errorf("%w: %w", domain.ErrHiddifyAPI, &domain.StatusError{Code: code}))
}

func activeCount(t *testing.T, f *fixture) int {
	t.Helper()
	list, err := f.uc.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(list)
}

// --- tests -----------------------------------------------------------------

func TestRedeem_SuccessFriend(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindFriend, 30)
	if len(iss.Code) != 11 || iss.Code[5] != '-' {
		t.Fatalf("code format = %q, want XXXXX-XXXXX", iss.Code)
	}
	if got := iss.ExpiresAt.Sub(f.clock.UTC()); got != 7*24*time.Hour {
		t.Fatalf("expires in %v, want 7 days", got)
	}

	res, err := f.redeem(42, iss.Code)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if res.SubscriptionURL == "" || res.Kind != invite.KindFriend || res.Days != 30 {
		t.Fatalf("unexpected result: %+v", res)
	}

	req := f.panel.createReqs[0]
	if req.Name != "alice" || req.TelegramID != 42 || req.UsageLimitGB != 200 || req.Mode != "monthly" || req.PackageDays != 30 {
		t.Fatalf("create request = %+v", req)
	}

	u, err := f.users.FindByTelegramID(context.Background(), 42)
	if err != nil {
		t.Fatalf("local user: %v", err)
	}
	if u.HiddifyUUID == "" || u.LinkSource != "invite" || !u.CanMessage || u.LinkedAt == nil {
		t.Fatalf("local user = %+v", u)
	}
	if n := activeCount(t, f); n != 0 {
		t.Fatalf("active invites = %d, want 0 after redeem", n)
	}
}

func TestRedeem_OwnTariffAndNameFallback(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, invite.Unlimited)
	if _, err := f.uc.Redeem(context.Background(), 7, "", iss.Code); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	req := f.panel.createReqs[0]
	if req.Name != "tg_7" || req.UsageLimitGB != 100000 || req.Mode != "no_reset" || req.PackageDays != 10000 {
		t.Fatalf("create request = %+v", req)
	}
}

func TestRedeem_Expired(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.advance(7*24*time.Hour + time.Second)
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("err = %v, want ErrInviteInvalid", err)
	}
	if f.panel.calls() != 0 {
		t.Fatal("panel must not be called")
	}
}

func TestRedeem_Revoked(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	if err := f.uc.Revoke(context.Background(), iss.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("err = %v, want ErrInviteInvalid", err)
	}
	if err := f.uc.Revoke(context.Background(), iss.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second revoke err = %v, want ErrNotFound", err)
	}
}

func TestRedeem_AlreadyRedeemed(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	if _, err := f.redeem(42, iss.Code); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := f.redeem(43, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("err = %v, want ErrInviteInvalid", err)
	}
	if f.panel.calls() != 1 {
		t.Fatalf("CreateUser calls = %d, want 1", f.panel.calls())
	}
}

func TestRedeem_WrongAttemptLimiter(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)

	for i := 0; i < 5; i++ {
		if _, err := f.redeem(42, "ZZZZZ-ZZZZZ"); !errors.Is(err, ErrInviteInvalid) {
			t.Fatalf("attempt %d: err = %v, want ErrInviteInvalid", i+1, err)
		}
	}
	before := f.invites.calls()
	// The 6th attempt is refused even with a valid code, and never reaches the invites table.
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRateLimited) {
		t.Fatalf("6th attempt err = %v, want ErrInviteRateLimited", err)
	}
	if f.invites.calls() != before {
		t.Fatal("limited attempt touched the invites repository")
	}
	// Another person is not affected.
	other := f.issue(t, invite.KindOwn, 7)
	if _, err := f.redeem(43, other.Code); err != nil {
		t.Fatalf("other person: %v", err)
	}
	// Once the window has passed the person may try again.
	f.advance(attemptWindow + time.Second)
	if _, err := f.redeem(42, iss.Code); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestRedeem_MalformedInputCountsAsWrongAndSkipsDB(t *testing.T) {
	f := newFixture(t)
	if _, err := f.redeem(42, "hello"); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("err = %v, want ErrInviteInvalid", err)
	}
	if f.invites.calls() != 0 {
		t.Fatal("malformed code must not reach the repository")
	}
}

func TestRedeem_TimeoutThenRetryFinishesWithoutSecondCreate(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindFriend, 7)

	// The panel creates the user but the answer is lost.
	f.panel.mu.Lock()
	f.panel.createErr, f.panel.createAnyway = timeoutErr(), true
	f.panel.mu.Unlock()

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("err = %v, want ErrInviteRetryLater", err)
	}
	// The claim is kept: nobody else can take the code.
	if _, err := f.redeem(43, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("other person err = %v, want ErrInviteInvalid", err)
	}
	if _, err := f.users.FindByTelegramID(context.Background(), 42); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("local user must not exist yet, err = %v", err)
	}

	f.panel.heal()
	f.advance(retryCooldown + time.Second)
	res, err := f.redeem(42, iss.Code)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if f.panel.calls() != 1 {
		t.Fatalf("CreateUser calls = %d, want 1", f.panel.calls())
	}
	u, err := f.users.FindByTelegramID(context.Background(), 42)
	if err != nil || u.HiddifyUUID != "fake-uuid-1" || u.LinkSource != "invite" {
		t.Fatalf("local user = %+v, err = %v", u, err)
	}
	if res.SubscriptionURL == "" {
		t.Fatal("subscription url must not be empty")
	}
	if res.SubscriptionURL != "https://panel.example/sub/fake-uuid-1/" {
		t.Fatalf("subscription url = %q", res.SubscriptionURL)
	}
	if n := activeCount(t, f); n != 0 {
		t.Fatalf("invite must be redeemed, active = %d", n)
	}
}

func TestRedeem_ServerErrorKeepsClaim(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.panel.fail(statusErr(502))

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("err = %v, want ErrInviteRetryLater", err)
	}
	if _, err := f.redeem(43, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("claim must be kept; other person err = %v", err)
	}
	f.panel.heal()
	f.advance(retryCooldown + time.Second)
	if _, err := f.redeem(42, iss.Code); err != nil {
		t.Fatalf("retry by the same person: %v", err)
	}
}

func TestRedeem_ClearRejectionReleasesClaim(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.panel.fail(statusErr(400))

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRejected) {
		t.Fatalf("err = %v, want ErrInviteRejected", err)
	}
	f.panel.heal()
	// Released: another person can redeem it.
	if _, err := f.redeem(43, iss.Code); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestRedeem_AlreadyLinkedRefusedWithoutClaim(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	now := time.Now()
	if err := f.users.Save(context.Background(), &user.User{TelegramID: 42, HiddifyUUID: "existing", LinkSource: "auto", CreatedAt: now}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Never counts as a wrong attempt: more than 5 refusals stay "already subscribed".
	for i := 0; i < 7; i++ {
		if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrAlreadySubscribed) {
			t.Fatalf("attempt %d: err = %v, want ErrAlreadySubscribed", i+1, err)
		}
	}
	if f.invites.calls() != 0 || f.panel.calls() != 0 {
		t.Fatal("nothing must be claimed or created")
	}
	if _, err := f.redeem(43, iss.Code); err != nil {
		t.Fatalf("code must stay usable for someone else: %v", err)
	}
}

func TestRedeem_PanelUserExistsWithoutEarlierClaimReleasesAndRefuses(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.panel.byTelegram[42] = "someone-elses-or-old"

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("err = %v, want ErrAlreadySubscribed", err)
	}
	if f.panel.calls() != 0 {
		t.Fatal("CreateUser must not be called")
	}
	list, _ := f.uc.ListActive(context.Background())
	if len(list) != 1 || list[0].ClaimedAt != nil {
		t.Fatalf("claim must be released, list = %+v", list)
	}
}

func TestRedeem_PanelLookupFailureNeverCreates(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.panel.lookupErr = fmt.Errorf("%w: boom", domain.ErrHiddifyAPI)

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("err = %v, want ErrInviteRetryLater", err)
	}
	if f.panel.calls() != 0 {
		t.Fatal("CreateUser must not be called when the lookup failed")
	}
	f.panel.lookupErr = nil
	if _, err := f.redeem(42, iss.Code); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestRedeem_ConcurrentSamePersonCreatesOnce(t *testing.T) {
	f := newFixture(t)
	f.panel.createDelay = 50 * time.Millisecond
	iss := f.issue(t, invite.KindOwn, 7)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.redeem(42, iss.Code)
		}()
	}
	wg.Wait()

	if f.panel.calls() != 1 {
		t.Fatalf("CreateUser calls = %d, want exactly 1", f.panel.calls())
	}
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrAlreadySubscribed):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("successes = %d, want 1 (errs = %v)", ok, errs)
	}
}

// lostCreate makes the panel create the user although CreateUser times out.
func (f *fixture) lostCreate() {
	f.panel.mu.Lock()
	defer f.panel.mu.Unlock()
	f.panel.createErr, f.panel.createAnyway = timeoutErr(), true
}

func TestRedeem_RetryInsideCooldownDoesNotTouchPanel(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindFriend, 7)
	f.lostCreate()

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("first: err = %v, want ErrInviteRetryLater", err)
	}
	lookups, creates := f.panel.lookups(), f.panel.calls()

	f.advance(time.Second)
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("second: err = %v, want ErrInviteRetryLater", err)
	}
	if f.panel.lookups() != lookups || f.panel.calls() != creates {
		t.Fatalf("panel touched inside cooldown: lookups %d->%d, creates %d->%d",
			lookups, f.panel.lookups(), creates, f.panel.calls())
	}

	f.advance(retryCooldown) // +61 s from the first claim
	res, err := f.redeem(42, iss.Code)
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if f.panel.calls() != 1 {
		t.Fatalf("CreateUser calls = %d, want 1", f.panel.calls())
	}
	if res.SubscriptionURL == "" {
		t.Fatal("subscription url must not be empty")
	}
}

func TestRedeem_EarlyRetriesDoNotExtendCooldown(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindFriend, 7)
	f.lostCreate()

	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("first: err = %v, want ErrInviteRetryLater", err)
	}
	lookups := f.panel.lookups()

	f.advance(time.Second) // +1 s
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("+1s: err = %v, want ErrInviteRetryLater", err)
	}
	f.advance(29 * time.Second) // +30 s
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("+30s: err = %v, want ErrInviteRetryLater", err)
	}
	if f.panel.lookups() != lookups {
		t.Fatal("panel must not be touched by early retries")
	}

	f.advance(31 * time.Second) // +61 s from the first claim, only 31 s after the last tap
	res, err := f.redeem(42, iss.Code)
	if err != nil {
		t.Fatalf("+61s: %v", err)
	}
	if res.SubscriptionURL == "" || f.panel.calls() != 1 {
		t.Fatalf("result = %+v, CreateUser calls = %d, want url and 1 call", res, f.panel.calls())
	}
}

func TestRedeem_ConcurrentDoubleTapAfterTimeoutCreatesOnce(t *testing.T) {
	f := newFixture(t)
	f.panel.createDelay = 50 * time.Millisecond
	f.lostCreate()
	iss := f.issue(t, invite.KindOwn, 7)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.redeem(42, iss.Code)
		}()
	}
	wg.Wait()

	if f.panel.calls() != 1 {
		t.Fatalf("CreateUser calls = %d, want exactly 1", f.panel.calls())
	}
	for i, err := range errs {
		if !errors.Is(err, ErrInviteRetryLater) {
			t.Fatalf("call %d: err = %v, want ErrInviteRetryLater", i, err)
		}
	}
}

func TestRedeem_AcceptsLooseInput(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	loose := " " + lower(iss.Code[:3]) + " " + lower(iss.Code[3:5]) + "  " + lower(iss.Code[6:]) + " "
	if _, err := f.redeem(42, loose); err != nil {
		t.Fatalf("redeem with %q: %v", loose, err)
	}
}

func TestRedeem_ConfusableLettersMapToDigits(t *testing.T) {
	f := newFixture(t)
	now := f.clock.UTC()
	// The stored code contains 0 and 1; the person types O, I and l.
	id, err := f.invites.Create(context.Background(),
		&invite.Invite{Kind: invite.KindOwn, Days: 7, CreatedAt: now, ExpiresAt: now.Add(invite.TTL)}, hashCode("01ABCDEF01"))
	if err != nil || id == 0 {
		t.Fatalf("seed invite: %v", err)
	}
	if _, err := f.redeem(42, "Ol-abcdef-Ol"); err != nil {
		t.Fatalf("redeem: %v", err)
	}
}

func TestRedeem_ClaimedByAnotherPersonRefused(t *testing.T) {
	f := newFixture(t)
	iss := f.issue(t, invite.KindOwn, 7)
	f.panel.fail(timeoutErr())
	if _, err := f.redeem(42, iss.Code); !errors.Is(err, ErrInviteRetryLater) {
		t.Fatalf("err = %v", err)
	}
	f.panel.heal()
	if _, err := f.redeem(43, iss.Code); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("err = %v, want ErrInviteInvalid", err)
	}
}

func TestIssue_Validation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.uc.Issue(context.Background(), "vip", 7); err == nil {
		t.Fatal("unknown kind must fail")
	}
	if _, err := f.uc.Issue(context.Background(), invite.KindOwn, 5); err == nil {
		t.Fatal("unsupported days must fail")
	}
}

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"K7QM4-XTP9A", "K7QM4XTP9A", true},
		{"k7qm4xtp9a", "K7QM4XTP9A", true},
		{" k7qm4 - xtp9a ", "K7QM4XTP9A", true},
		{"O0-IL-OOIL0Z", "001100110Z", true}, // O->0, I/L->1
		{"K7QM4-XTP9", "K7QM4XTP9", false},   // too short
		{"K7QM4-XTP9AA", "K7QM4XTP9AA", false},
		{"U7QM4-XTP9A", "U7QM4XTP9A", false}, // U is not in the alphabet
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeCode(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
