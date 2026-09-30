package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/sqlite"
)

var inviteNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newInvite(kind invite.Kind, days int) *invite.Invite {
	return &invite.Invite{Kind: kind, Days: days, CreatedAt: inviteNow, ExpiresAt: inviteNow.Add(invite.TTL)}
}

func TestInviteRepository_ClaimLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewInviteRepository(openTestDB(t))

	id, err := repo.Create(ctx, newInvite(invite.KindFriend, 30), "hash-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := repo.Claim(ctx, "hash-1", 42, inviteNow)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if first.Retry || first.Invite.ID != id || first.Invite.Kind != invite.KindFriend || first.Invite.Days != 30 {
		t.Fatalf("first claim = %+v", first)
	}

	// Someone else is refused while the claim is held.
	if _, err := repo.Claim(ctx, "hash-1", 43, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other person err = %v, want ErrNotFound", err)
	}
	// The same person retrying is recognised.
	again, err := repo.Claim(ctx, "hash-1", 42, inviteNow.Add(time.Minute))
	if err != nil || !again.Retry {
		t.Fatalf("retry claim = %+v, err = %v", again, err)
	}

	// Release hands the code back.
	if err := repo.Release(ctx, id); err != nil {
		t.Fatalf("release: %v", err)
	}
	other, err := repo.Claim(ctx, "hash-1", 43, inviteNow)
	if err != nil || other.Retry {
		t.Fatalf("claim after release = %+v, err = %v", other, err)
	}

	if err := repo.Complete(ctx, id, "uuid-1", inviteNow); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := repo.Claim(ctx, "hash-1", 43, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("claim after redeem err = %v, want ErrNotFound", err)
	}
	// Release must not reopen a redeemed invite.
	if err := repo.Release(ctx, id); err != nil {
		t.Fatalf("release redeemed: %v", err)
	}
	if _, err := repo.Claim(ctx, "hash-1", 44, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("redeemed invite was reopened, err = %v", err)
	}
}

func TestInviteRepository_ClaimRespectsExpiryRevokeAndUnknown(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewInviteRepository(openTestDB(t))

	if _, err := repo.Claim(ctx, "nope", 1, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown err = %v", err)
	}

	if _, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "h-exp"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Claim(ctx, "h-exp", 1, inviteNow.Add(invite.TTL)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired (at the boundary) err = %v", err)
	}
	if _, err := repo.Claim(ctx, "h-exp", 1, inviteNow.Add(invite.TTL-time.Second)); err != nil {
		t.Fatalf("one second before expiry: %v", err)
	}

	id, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "h-rev")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Revoke(ctx, id, inviteNow); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := repo.Claim(ctx, "h-rev", 1, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoked err = %v", err)
	}
	if err := repo.Revoke(ctx, id, inviteNow); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("double revoke err = %v", err)
	}
}

func TestInviteRepository_ListActive(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewInviteRepository(openTestDB(t))

	idActive, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idRevoked, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "b")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	idDone, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "c")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	old := newInvite(invite.KindFriend, 1)
	old.CreatedAt, old.ExpiresAt = inviteNow.Add(-10*24*time.Hour), inviteNow.Add(-3*24*time.Hour)
	if _, err := repo.Create(ctx, old, "d"); err != nil {
		t.Fatalf("create old: %v", err)
	}
	if err := repo.Revoke(ctx, idRevoked, inviteNow); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := repo.Complete(ctx, idDone, "u", inviteNow); err != nil {
		t.Fatalf("complete: %v", err)
	}

	list, err := repo.ListActive(ctx, inviteNow)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != idActive || list[0].Kind != invite.KindOwn || list[0].Days != 7 {
		t.Fatalf("list = %+v", list)
	}
	if !list[0].ExpiresAt.Equal(inviteNow.Add(invite.TTL)) {
		t.Fatalf("expires_at = %v", list[0].ExpiresAt)
	}
}

func TestInviteRepository_ClaimExposesPrevClaimedAtAndKeepsOriginal(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewInviteRepository(openTestDB(t))
	if _, err := repo.Create(ctx, newInvite(invite.KindOwn, 7), "hash-prev"); err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := repo.Claim(ctx, "hash-prev", 42, inviteNow)
	if err != nil || first.Retry || !first.PrevClaimedAt.IsZero() {
		t.Fatalf("first claim = %+v, err = %v", first, err)
	}

	// Early retries see the first claim time and do not move it.
	for _, d := range []time.Duration{time.Second, 30 * time.Second} {
		got, err := repo.Claim(ctx, "hash-prev", 42, inviteNow.Add(d))
		if err != nil || !got.Retry || !got.PrevClaimedAt.Equal(inviteNow) {
			t.Fatalf("early retry +%v = %+v, err = %v", d, got, err)
		}
		if !got.Invite.ClaimedAt.Equal(inviteNow) {
			t.Fatalf("early retry +%v: ClaimedAt = %v, want the original %v", d, got.Invite.ClaimedAt, inviteNow)
		}
	}

	// After the cooldown the retry still sees the original time, then the clock restarts.
	late := inviteNow.Add(invite.RetryCooldown + time.Second)
	got, err := repo.Claim(ctx, "hash-prev", 42, late)
	if err != nil || !got.Retry || !got.PrevClaimedAt.Equal(inviteNow) || !got.Invite.ClaimedAt.Equal(late) {
		t.Fatalf("late retry = %+v, err = %v", got, err)
	}
	next, err := repo.Claim(ctx, "hash-prev", 42, late.Add(time.Second))
	if err != nil || !next.PrevClaimedAt.Equal(late) {
		t.Fatalf("retry after restart = %+v, err = %v", next, err)
	}
}
