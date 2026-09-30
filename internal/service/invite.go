package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
)

const (
	// Crockford base32: no I, L, O, U.
	codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	codeLength   = 10

	maxWrongAttempts = 5
	attemptWindow    = 10 * time.Minute

	// retryCooldown: a retry this soon after the previous attempt is answered
	// "try later" without any panel call (Hiddify's apply takes ~10 s, the HTTP
	// timeout is 15 s; the earlier CreateUser may still be in flight there).
	retryCooldown = invite.RetryCooldown
)

// Outcomes of Redeem that the transport turns into user-facing texts.
var (
	// ErrInviteInvalid covers unknown, expired, revoked, redeemed and foreign codes
	// (one generic answer on purpose).
	ErrInviteInvalid = errors.New("invite: code is not valid")
	// ErrInviteRateLimited: too many wrong attempts in the window.
	ErrInviteRateLimited = errors.New("invite: too many wrong attempts")
	// ErrAlreadySubscribed: the person already has a subscription (D2: one per person).
	ErrAlreadySubscribed = errors.New("invite: already has a subscription")
	// ErrInviteRetryLater: outcome unknown (timeout, 5xx, network); the claim is kept.
	ErrInviteRetryLater = errors.New("invite: try again later")
	// ErrInviteRejected: the panel clearly refused; the claim was released.
	ErrInviteRejected = errors.New("invite: panel rejected the request")
)

// Issued is a freshly created code. Code is shown once and never stored.
type Issued struct {
	ID        int64
	Code      string // XXXXX-XXXXX
	Kind      invite.Kind
	Days      int
	ExpiresAt time.Time
}

// PayloadCode is the code without the hyphen (for t.me deep links).
func (i *Issued) PayloadCode() string { return strings.ReplaceAll(i.Code, "-", "") }

// RedeemResult is returned on a successful redemption.
type RedeemResult struct {
	InviteID        int64
	Kind            invite.Kind
	Days            int
	SubscriptionURL string
}

// InviteUseCase issues and redeems one-time invite codes.
type InviteUseCase struct {
	invites invite.Repository
	users   user.Repository
	panel   subscription.Repository
	log     *slog.Logger
	now     func() time.Time

	// mu serializes the whole Redeem (telebot runs handlers concurrently; a
	// double tap must not produce two CreateUser calls) and guards wrong.
	mu    sync.Mutex
	wrong map[int64][]time.Time
}

func NewInviteUseCase(invites invite.Repository, users user.Repository, panel subscription.Repository, log *slog.Logger) *InviteUseCase {
	return &InviteUseCase{
		invites: invites,
		users:   users,
		panel:   panel,
		log:     log.With("service", "invite"),
		now:     time.Now,
		wrong:   make(map[int64][]time.Time),
	}
}

// NormalizeCode uppercases, drops spaces and hyphens and maps O->0, I/L->1.
// ok is false if the result is not a well-formed code.
func NormalizeCode(raw string) (code string, ok bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		switch {
		case r == '-' || unicode.IsSpace(r):
			continue
		case r == 'O':
			r = '0'
		case r == 'I' || r == 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	code = b.String()
	if len(code) != codeLength {
		return code, false
	}
	for _, r := range code {
		if !strings.ContainsRune(codeAlphabet, r) {
			return code, false
		}
	}
	return code, true
}

func hashCode(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func generateCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	out := make([]byte, codeLength)
	for i, b := range buf {
		out[i] = codeAlphabet[int(b)%len(codeAlphabet)] // 256 % 32 == 0: no bias
	}
	return string(out), nil
}

// Issue creates a one-time code valid for invite.TTL.
func (uc *InviteUseCase) Issue(ctx context.Context, kind invite.Kind, days int) (*Issued, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("issue invite: unknown kind %q", kind)
	}
	if !invite.ValidDays(days) {
		return nil, fmt.Errorf("issue invite: unsupported days %d", days)
	}
	code, err := generateCode()
	if err != nil {
		return nil, fmt.Errorf("issue invite: %w", err)
	}
	now := uc.now().UTC().Truncate(time.Second)
	inv := &invite.Invite{Kind: kind, Days: days, CreatedAt: now, ExpiresAt: now.Add(invite.TTL)}
	id, err := uc.invites.Create(ctx, inv, hashCode(code))
	if err != nil {
		return nil, fmt.Errorf("issue invite: %w", err)
	}
	uc.log.Info("invite issued", "invite_id", id, "kind", kind, "days", days)
	return &Issued{
		ID:        id,
		Code:      code[:codeLength/2] + "-" + code[codeLength/2:],
		Kind:      kind,
		Days:      days,
		ExpiresAt: inv.ExpiresAt,
	}, nil
}

// ListActive returns codes that are not expired, revoked or redeemed.
func (uc *InviteUseCase) ListActive(ctx context.Context) ([]*invite.Invite, error) {
	list, err := uc.invites.ListActive(ctx, uc.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return list, nil
}

// Revoke cancels an active code. domain.ErrNotFound if it is already gone.
func (uc *InviteUseCase) Revoke(ctx context.Context, id int64) error {
	if err := uc.invites.Revoke(ctx, id, uc.now().UTC()); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	uc.log.Info("invite revoked", "invite_id", id)
	return nil
}

// Redeem exchanges a code for a panel subscription. The whole call is
// serialized. Logs carry only the invite row id: never the code, telegram id or uuid.
func (uc *InviteUseCase) Redeem(ctx context.Context, telegramID int64, username, rawCode string) (*RedeemResult, error) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	now := uc.now().UTC().Truncate(time.Second)

	// b. One subscription per person; not a wrong attempt, nothing is claimed.
	local, err := uc.users.FindByTelegramID(ctx, telegramID)
	switch {
	case err == nil:
		if local.IsLinked() {
			return nil, ErrAlreadySubscribed
		}
	case errors.Is(err, domain.ErrNotFound):
		local = nil
	default:
		return nil, fmt.Errorf("redeem: find user: %w", err)
	}

	// c. Attempt limiter (memory only).
	if uc.wrongCount(telegramID, now) >= maxWrongAttempts {
		return nil, ErrInviteRateLimited
	}

	code, ok := NormalizeCode(rawCode)
	if !ok {
		uc.addWrong(telegramID, now)
		return nil, ErrInviteInvalid
	}

	// d. Atomic claim.
	claim, err := uc.invites.Claim(ctx, hashCode(code), telegramID, now)
	if errors.Is(err, domain.ErrNotFound) {
		uc.addWrong(telegramID, now)
		return nil, ErrInviteInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("redeem: claim: %w", err)
	}
	inv := claim.Invite

	// d2. Too soon after the previous attempt: the panel may still be creating
	// the user, so neither look up nor create. (The repository keeps the
	// original claim time here, so early taps do not extend the cooldown.)
	if claim.Retry && now.Sub(claim.PrevClaimedAt) < retryCooldown {
		uc.log.Info("invite: retry inside cooldown, panel not touched", "invite_id", inv.ID)
		return nil, ErrInviteRetryLater
	}

	// e. Is there already a panel user for this person?
	existing, uuid, err := uc.panel.GetUserByTelegramID(ctx, telegramID)
	switch {
	case err == nil:
		if claim.Retry {
			uc.log.Info("invite: finishing on existing panel user after earlier attempt", "invite_id", inv.ID)
			return uc.finish(ctx, local, telegramID, username, inv, uuid, existing.SubscriptionURL, now)
		}
		uc.release(ctx, inv.ID)
		return nil, ErrAlreadySubscribed
	case errors.Is(err, domain.ErrNotFound):
		// nothing in the panel: create below
	default:
		uc.log.Warn("invite: panel lookup failed", "invite_id", inv.ID, "err", err)
		if !claim.Retry {
			uc.release(ctx, inv.ID)
		}
		return nil, ErrInviteRetryLater
	}

	// f. Create in the panel through the same CreateUser as the approve flow.
	name := username
	if name == "" {
		name = fmt.Sprintf("tg_%d", telegramID)
	}
	gb, mode := inv.Kind.Tariff()
	created, err := uc.panel.CreateUser(ctx, subscription.CreateUserRequest{
		Name:         name,
		TelegramID:   telegramID,
		UsageLimitGB: gb,
		PackageDays:  inv.Days,
		Mode:         mode,
	})
	if err != nil {
		if domain.IsClearRejection(err) {
			uc.log.Warn("invite: panel rejected create, claim released", "invite_id", inv.ID, "err", err)
			uc.release(ctx, inv.ID)
			return nil, ErrInviteRejected
		}
		// Timeout / 5xx / network: the panel may have created the user anyway.
		uc.log.Warn("invite: panel create outcome unknown, claim kept", "invite_id", inv.ID, "err", err)
		return nil, ErrInviteRetryLater
	}
	return uc.finish(ctx, local, telegramID, username, inv, created.UUID, created.SubscriptionURL, now)
}

// finish links the local user and marks the invite redeemed. If saving the
// user fails the claim stays, so a retry finds the panel user and finishes.
func (uc *InviteUseCase) finish(ctx context.Context, local *user.User, telegramID int64, username string, inv invite.Invite, uuid, subURL string, now time.Time) (*RedeemResult, error) {
	if local == nil {
		local = &user.User{TelegramID: telegramID, CreatedAt: now}
	}
	local.Username = username
	local.HiddifyUUID = uuid
	local.LinkSource = "invite"
	local.LinkedAt = &now
	local.LastSeen = &now
	local.CanMessage = true
	if err := uc.users.Save(ctx, local); err != nil {
		uc.log.Error("invite: save local user failed, claim kept", "invite_id", inv.ID, "err", err)
		return nil, ErrInviteRetryLater
	}
	if err := uc.invites.Complete(ctx, inv.ID, uuid, now); err != nil {
		// The person is linked already; only the bookkeeping failed.
		uc.log.Error("invite: mark redeemed failed", "invite_id", inv.ID, "err", err)
	}
	uc.log.Info("invite redeemed", "invite_id", inv.ID)
	return &RedeemResult{InviteID: inv.ID, Kind: inv.Kind, Days: inv.Days, SubscriptionURL: subURL}, nil
}

func (uc *InviteUseCase) release(ctx context.Context, id int64) {
	if err := uc.invites.Release(ctx, id); err != nil {
		uc.log.Error("invite: release claim failed", "invite_id", id, "err", err)
	}
}

// wrongCount drops attempts older than the window and returns how many remain.
// Callers hold uc.mu.
func (uc *InviteUseCase) wrongCount(telegramID int64, now time.Time) int {
	kept := uc.wrong[telegramID][:0]
	for _, t := range uc.wrong[telegramID] {
		if now.Sub(t) < attemptWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(uc.wrong, telegramID)
		return 0
	}
	uc.wrong[telegramID] = kept
	return len(kept)
}

func (uc *InviteUseCase) addWrong(telegramID int64, now time.Time) {
	uc.wrong[telegramID] = append(uc.wrong[telegramID], now)
}
