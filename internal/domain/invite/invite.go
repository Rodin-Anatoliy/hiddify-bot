// Package invite holds the invite-code domain: kinds, tariffs and the repository contract.
package invite

import (
	"context"
	"time"
)

// Kind is the tariff family of an invite.
type Kind string

const (
	KindOwn    Kind = "own"    // own devices: no traffic limit, no reset
	KindFriend Kind = "friend" // friends: 200 GB per month
)

// Unlimited is the package_days value the panel treats as "no expiry".
const Unlimited = 10000

// DefaultDays is the pre-selected term in the admin wizard.
const DefaultDays = Unlimited

// TTL is how long an unredeemed code stays valid.
const TTL = 7 * 24 * time.Hour

// DayChoices are the allowed subscription lengths, in days.
var DayChoices = []int{1, 7, 30, 180, Unlimited}

func ValidDays(days int) bool {
	for _, d := range DayChoices {
		if d == days {
			return true
		}
	}
	return false
}

func (k Kind) Valid() bool { return k == KindOwn || k == KindFriend }

// Label is the Russian name shown to the admin.
func (k Kind) Label() string {
	if k == KindFriend {
		return "Знакомый"
	}
	return "Свой"
}

// Tariff returns the panel limits for the kind: traffic in GB and reset mode
// (mode strings are the panel's UserMode values).
func (k Kind) Tariff() (usageLimitGB int, mode string) {
	if k == KindFriend {
		return 200, "monthly"
	}
	return 100000, "no_reset"
}

// DaysLabel is a short Russian name of a term.
func DaysLabel(days int) string {
	switch days {
	case 1:
		return "день"
	case 7:
		return "неделя"
	case 30:
		return "месяц"
	case 180:
		return "полгода"
	case Unlimited:
		return "бессрочно"
	}
	return "срок не определён"
}

// Invite is one stored code (without the code itself: only its hash is kept).
type Invite struct {
	ID          int64
	Kind        Kind
	Days        int
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ClaimedAt   *time.Time
	ClaimedByTG *int64
}

// RetryCooldown is how long after the last claim that actually proceeded to
// the panel a retry by the same person is turned away without touching the
// panel (the panel may still be applying the earlier create).
const RetryCooldown = 60 * time.Second

// Claim is the result of a successful claim.
type Claim struct {
	Invite Invite
	// Retry is true when the same person had already claimed this invite
	// before this call (an earlier attempt did not finish).
	Retry bool
	// PrevClaimedAt is the claim time stored before this call; zero when
	// there was no earlier claim. A retry made less than RetryCooldown after
	// it keeps the stored time (so early taps do not extend the cooldown);
	// a later retry moves it to "now" (the new attempt starts its own cooldown).
	PrevClaimedAt time.Time
}

// Repository stores invites. Times are compared as UTC seconds.
// Claim returns domain.ErrNotFound when the code is unknown, expired, revoked,
// already redeemed or claimed by someone else.
type Repository interface {
	Create(ctx context.Context, inv *Invite, codeHash string) (int64, error)
	Claim(ctx context.Context, codeHash string, telegramID int64, now time.Time) (*Claim, error)
	Release(ctx context.Context, id int64) error
	Complete(ctx context.Context, id int64, uuid string, now time.Time) error
	ListActive(ctx context.Context, now time.Time) ([]*Invite, error)
	// Revoke returns domain.ErrNotFound if the invite is missing or already redeemed/revoked.
	Revoke(ctx context.Context, id int64, now time.Time) error
}
