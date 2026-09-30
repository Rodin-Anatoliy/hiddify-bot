package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/invite"
)

// InviteRepository stores invite codes. All timestamps are Unix seconds
// (INTEGER), so comparisons in SQL are numeric and time-zone independent.
type InviteRepository struct{ db *DB }

func NewInviteRepository(db *DB) *InviteRepository { return &InviteRepository{db: db} }

func (r *InviteRepository) Create(ctx context.Context, inv *invite.Invite, codeHash string) (int64, error) {
	res, err := r.db.conn.ExecContext(ctx,
		`INSERT INTO invites (code_hash, kind, days, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		codeHash, string(inv.Kind), inv.Days, inv.CreatedAt.Unix(), inv.ExpiresAt.Unix())
	if err != nil {
		return 0, fmt.Errorf("invite create: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("invite create: last id: %w", err)
	}
	return id, nil
}

// Claim marks the invite as taken by telegramID. The previous claim state is
// read in the same transaction (the pool has a single connection, so nothing
// interleaves); the claim itself is one conditional UPDATE.
func (r *InviteRepository) Claim(ctx context.Context, codeHash string, telegramID int64, now time.Time) (*invite.Claim, error) {
	tx, err := r.db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("invite claim: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var (
		inv       invite.Invite
		kind      string
		created   int64
		expires   int64
		prevClaim sql.NullInt64
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, kind, days, created_at, expires_at, claimed_at FROM invites WHERE code_hash = ?`, codeHash,
	).Scan(&inv.ID, &kind, &inv.Days, &created, &expires, &prevClaim)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("invite claim: lookup: %w", err)
	}

	ts := now.Unix()
	// claimed_at moves to "now" only for a first claim or a retry after the
	// cooldown; an early retry keeps the stored time so repeated taps cannot
	// stretch the cooldown.
	cooldown := int64(invite.RetryCooldown / time.Second)
	res, err := tx.ExecContext(ctx,
		`UPDATE invites SET claimed_at = CASE WHEN claimed_at IS NULL OR ? - claimed_at >= ? THEN ? ELSE claimed_at END,
		   claimed_by_tg = ?
		 WHERE code_hash = ? AND redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ?
		   AND (claimed_at IS NULL OR claimed_by_tg = ?)`,
		ts, cooldown, ts, telegramID, codeHash, ts, telegramID)
	if err != nil {
		return nil, fmt.Errorf("invite claim: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("invite claim: rows: %w", err)
	}
	if n == 0 {
		return nil, domain.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("invite claim: commit: %w", err)
	}

	inv.Kind = invite.Kind(kind)
	inv.CreatedAt = time.Unix(created, 0).UTC()
	inv.ExpiresAt = time.Unix(expires, 0).UTC()
	claimedAt := now.UTC()
	var prevAt time.Time
	if prevClaim.Valid {
		prevAt = time.Unix(prevClaim.Int64, 0).UTC()
		if ts-prevClaim.Int64 < cooldown {
			claimedAt = prevAt // kept by the UPDATE above
		}
	}
	inv.ClaimedAt = &claimedAt
	inv.ClaimedByTG = &telegramID
	return &invite.Claim{Invite: inv, Retry: prevClaim.Valid, PrevClaimedAt: prevAt}, nil
}

func (r *InviteRepository) Release(ctx context.Context, id int64) error {
	_, err := r.db.conn.ExecContext(ctx,
		`UPDATE invites SET claimed_at = NULL, claimed_by_tg = NULL WHERE id = ? AND redeemed_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("invite release: %w", err)
	}
	return nil
}

func (r *InviteRepository) Complete(ctx context.Context, id int64, uuid string, now time.Time) error {
	_, err := r.db.conn.ExecContext(ctx,
		`UPDATE invites SET redeemed_at = ?, redeemed_uuid = ? WHERE id = ?`, now.Unix(), uuid, id)
	if err != nil {
		return fmt.Errorf("invite complete: %w", err)
	}
	return nil
}

func (r *InviteRepository) ListActive(ctx context.Context, now time.Time) ([]*invite.Invite, error) {
	rows, err := r.db.conn.QueryContext(ctx,
		`SELECT id, kind, days, created_at, expires_at, claimed_at, claimed_by_tg FROM invites
		 WHERE redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ? ORDER BY id`, now.Unix())
	if err != nil {
		return nil, fmt.Errorf("invite list: %w", err)
	}
	defer rows.Close()

	var out []*invite.Invite
	for rows.Next() {
		var (
			inv            invite.Invite
			kind           string
			created, exp   int64
			claimed, claim sql.NullInt64
		)
		if err := rows.Scan(&inv.ID, &kind, &inv.Days, &created, &exp, &claimed, &claim); err != nil {
			return nil, fmt.Errorf("invite list: scan: %w", err)
		}
		inv.Kind = invite.Kind(kind)
		inv.CreatedAt = time.Unix(created, 0).UTC()
		inv.ExpiresAt = time.Unix(exp, 0).UTC()
		if claimed.Valid {
			t := time.Unix(claimed.Int64, 0).UTC()
			inv.ClaimedAt = &t
		}
		if claim.Valid {
			v := claim.Int64
			inv.ClaimedByTG = &v
		}
		out = append(out, &inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invite list: %w", err)
	}
	return out, nil
}

func (r *InviteRepository) Revoke(ctx context.Context, id int64, now time.Time) error {
	res, err := r.db.conn.ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND redeemed_at IS NULL AND revoked_at IS NULL`, now.Unix(), id)
	if err != nil {
		return fmt.Errorf("invite revoke: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("invite revoke: rows: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

var _ invite.Repository = (*InviteRepository)(nil)
