package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/errs"
)

// CreateUserRequest holds parameters for creating a new Hiddify user.
type CreateUserRequest struct {
	Name       string
	TelegramID int64
}

// CreatedUser is returned after successfully creating a user in the panel.
type CreatedUser struct {
	UUID            string
	SubscriptionURL string
	ExpiresAt       time.Time
}

// HiddifyClient is the subset of the Hiddify API used by this use case.
// The interface is defined here (not in infrastructure) so use cases stay testable.
type HiddifyClient interface {
	GetUserByUUID(ctx context.Context, uuid string) (*subscription.Status, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*subscription.Status, string, error)
	// ListPanelUsers returns a lightweight list for sync purposes.
	ListPanelUsers(ctx context.Context) ([]PanelUserDTO, error)
	SetTelegramID(ctx context.Context, uuid string, telegramID int64) error
	CreateUser(ctx context.Context, req CreateUserRequest) (*CreatedUser, error)
}

// PanelUserDTO carries the minimum data needed for /sync.
// Defined here to keep the usecase layer free from infrastructure imports.
type PanelUserDTO struct {
	UUID       string
	Name       string
	TelegramID *int64
}

// PanelUserView is an admin-facing view of a Hiddify user enriched with local bot state.
type PanelUserView struct {
	UUID       string
	Name       string
	TelegramID *int64
	KnownToBot bool
	CanMessage bool
}

// SyncResult holds statistics from a Hiddify→local sync operation.
type SyncResult struct {
	Total   int
	Linked  int
	Created int
	Updated int
	Skipped int
}

// RegistrationResult tells the transport how to greet the user after /start.
type RegistrationResult struct {
	User      *user.User
	FirstSeen bool
}

// UserUseCase orchestrates user registration, linking, and subscription retrieval.
type UserUseCase struct {
	users   user.Repository
	hiddify HiddifyClient
	log     *slog.Logger
}

func NewUserUseCase(users user.Repository, hiddify HiddifyClient, log *slog.Logger) *UserUseCase {
	return &UserUseCase{
		users:   users,
		hiddify: hiddify,
		log:     log.With("usecase", "user"),
	}
}

// RegisterOrGet ensures a local record exists for this Telegram user, updates LastSeen,
// and attempts auto-linking with Hiddify on every call (in case panel was updated).
func (uc *UserUseCase) RegisterOrGet(ctx context.Context, telegramID int64, username string) (*user.User, error) {
	result, err := uc.RegisterOrGetWithState(ctx, telegramID, username)
	if err != nil {
		return nil, err
	}
	return result.User, nil
}

// RegisterOrGetWithState is RegisterOrGet plus information about whether this
// is the first time the bot sees this Telegram user.
func (uc *UserUseCase) RegisterOrGetWithState(ctx context.Context, telegramID int64, username string) (*RegistrationResult, error) {
	now := time.Now()
	firstSeen := false

	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, fmt.Errorf("register: lookup: %w", err)
	}

	if errors.Is(err, errs.ErrNotFound) {
		firstSeen = true
		u = &user.User{
			TelegramID: telegramID,
			CreatedAt:  now,
		}
	}

	u.Username = username
	u.CanMessage = true
	u.LastSeen = &now

	if !u.IsLinked() {
		uc.tryAutoLink(ctx, u, now)
	}

	if saveErr := uc.users.Save(ctx, u); saveErr != nil {
		return nil, fmt.Errorf("register: save: %w", saveErr)
	}
	return &RegistrationResult{User: u, FirstSeen: firstSeen}, nil
}

// tryAutoLink attempts to find a matching Hiddify user by telegram_id and link them.
// It mutates u in place; errors are logged but not returned (non-fatal).
func (uc *UserUseCase) tryAutoLink(ctx context.Context, u *user.User, now time.Time) {
	_, uuid, err := uc.hiddify.GetUserByTelegramID(ctx, u.TelegramID)
	if err != nil {
		return
	}
	u.HiddifyUUID = uuid
	u.LinkSource = "auto"
	u.LinkedAt = &now
	uc.log.Info("user auto-linked", "telegram_id", u.TelegramID, "uuid", uuid)
}

// LinkManually binds a specific Hiddify UUID to a Telegram ID.
// Creates a local user record if one doesn't exist yet.
// Called by admin via /bind command.
func (uc *UserUseCase) LinkManually(ctx context.Context, telegramID int64, uuid string) error {
	if _, err := uc.hiddify.GetUserByUUID(ctx, uuid); err != nil {
		return fmt.Errorf("link: validate uuid: %w", err)
	}

	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if errors.Is(err, errs.ErrNotFound) {
		u = &user.User{
			TelegramID: telegramID,
			CanMessage: false, // user hasn't pressed /start yet
			CreatedAt:  time.Now(),
		}
	} else if err != nil {
		return fmt.Errorf("link: find user: %w", err)
	}

	now := time.Now()
	u.HiddifyUUID = uuid
	u.LinkSource = "admin"
	u.LinkedAt = &now

	if err := uc.users.Save(ctx, u); err != nil {
		return fmt.Errorf("link: save: %w", err)
	}

	// Propagate to panel — non-fatal if it fails.
	if err := uc.hiddify.SetTelegramID(ctx, uuid, telegramID); err != nil {
		uc.log.Warn("link: could not set telegram_id in panel", "err", err)
	}
	return nil
}

// GetSubscription returns the live subscription status for a linked user.
func (uc *UserUseCase) GetSubscription(ctx context.Context, telegramID int64) (*subscription.Status, error) {
	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if !u.IsLinked() {
		return nil, errs.ErrNotFound
	}
	return uc.hiddify.GetUserByUUID(ctx, u.HiddifyUUID)
}

// MarkCanMessage records whether Telegram delivery is currently possible.
func (uc *UserUseCase) MarkCanMessage(ctx context.Context, telegramID int64, canMessage bool) error {
	if err := uc.users.SetCanMessage(ctx, telegramID, canMessage); err != nil {
		return fmt.Errorf("user mark can_message: %w", err)
	}
	return nil
}

// SyncFromHiddify pulls all panel users with a telegram_id and upserts local records.
// Users created this way have CanMessage=false until they press /start.
func (uc *UserUseCase) SyncFromHiddify(ctx context.Context) (*SyncResult, error) {
	panelUsers, err := uc.hiddify.ListPanelUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}

	result := &SyncResult{Total: len(panelUsers)}
	now := time.Now()

	for _, pu := range panelUsers {
		if pu.TelegramID == nil || *pu.TelegramID == 0 {
			result.Skipped++
			continue
		}
		result.Linked++

		if err := uc.upsertFromPanel(ctx, pu, now, result); err != nil {
			return nil, err
		}
	}

	uc.log.Info("sync completed", "total", result.Total, "created", result.Created, "updated", result.Updated)
	return result, nil
}

func (uc *UserUseCase) upsertFromPanel(ctx context.Context, pu PanelUserDTO, now time.Time, result *SyncResult) error {
	telegramID := *pu.TelegramID

	existing, err := uc.users.FindByTelegramID(ctx, telegramID)
	if errors.Is(err, errs.ErrNotFound) {
		u := &user.User{
			TelegramID:  telegramID,
			HiddifyUUID: pu.UUID,
			Username:    pu.Name,
			LinkSource:  "sync",
			LinkedAt:    &now,
			CanMessage:  true,
			CreatedAt:   now,
		}
		if saveErr := uc.users.Save(ctx, u); saveErr != nil {
			return fmt.Errorf("sync: create: %w", saveErr)
		}
		result.Created++
		return nil
	}
	if err != nil {
		return fmt.Errorf("sync: find: %w", err)
	}

	changed := false
	if existing.HiddifyUUID != pu.UUID {
		existing.HiddifyUUID = pu.UUID
		existing.LinkedAt = &now
		changed = true
	}
	if existing.Username == "" && pu.Name != "" {
		existing.Username = pu.Name
		changed = true
	}
	if changed {
		if saveErr := uc.users.Save(ctx, existing); saveErr != nil {
			return fmt.Errorf("sync: update: %w", saveErr)
		}
		result.Updated++
	}
	return nil
}

// ListLinked returns all users with a Hiddify UUID — for admin /users command.
// Includes users who haven't pressed /start yet (CanMessage=false).
func (uc *UserUseCase) ListLinked(ctx context.Context) ([]*user.User, error) {
	return uc.users.FindAllWithUUID(ctx)
}

// ListPanelUserViews returns all Hiddify users enriched with local Telegram bot state.
func (uc *UserUseCase) ListPanelUserViews(ctx context.Context) ([]PanelUserView, error) {
	panelUsers, err := uc.hiddify.ListPanelUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list panel users: %w", err)
	}

	out := make([]PanelUserView, 0, len(panelUsers))
	for _, pu := range panelUsers {
		view := PanelUserView{
			UUID:       pu.UUID,
			Name:       pu.Name,
			TelegramID: pu.TelegramID,
		}
		if pu.TelegramID != nil && *pu.TelegramID != 0 {
			u, findErr := uc.users.FindByTelegramID(ctx, *pu.TelegramID)
			switch {
			case findErr == nil:
				view.KnownToBot = true
				view.CanMessage = u.CanMessage
			case errors.Is(findErr, errs.ErrNotFound):
				// User exists in Hiddify with telegram_id, but has not touched the bot yet.
			default:
				return nil, fmt.Errorf("list panel users: local lookup: %w", findErr)
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// ListUnboundPanelUsers returns Hiddify users without telegram_id.
func (uc *UserUseCase) ListUnboundPanelUsers(ctx context.Context) ([]PanelUserDTO, error) {
	panelUsers, err := uc.hiddify.ListPanelUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unbound: %w", err)
	}

	out := make([]PanelUserDTO, 0)
	for _, pu := range panelUsers {
		if pu.TelegramID == nil || *pu.TelegramID == 0 {
			out = append(out, pu)
		}
	}
	return out, nil
}

// ApproveAccessRequest creates a new Hiddify user, links them in local DB,
// and returns the created user data so the bot can notify them.
func (uc *UserUseCase) ApproveAccessRequest(ctx context.Context, telegramID int64, username string) (*CreatedUser, error) {
	name := username
	if name == "" {
		name = fmt.Sprintf("tg_%d", telegramID)
	}

	created, err := uc.hiddify.CreateUser(ctx, CreateUserRequest{
		Name:       name,
		TelegramID: telegramID,
	})
	if err != nil {
		return nil, fmt.Errorf("approve access: create in panel: %w", err)
	}

	now := time.Now()
	u, findErr := uc.users.FindByTelegramID(ctx, telegramID)
	if errors.Is(findErr, errs.ErrNotFound) {
		u = &user.User{
			TelegramID: telegramID,
			Username:   username,
			CreatedAt:  now,
		}
	} else if findErr != nil {
		return nil, fmt.Errorf("approve access: find user: %w", findErr)
	}

	u.HiddifyUUID = created.UUID
	u.LinkSource = "approved"
	u.LinkedAt = &now

	if err := uc.users.Save(ctx, u); err != nil {
		return nil, fmt.Errorf("approve access: save: %w", err)
	}

	return created, nil
}

// GetUser returns a local user record by Telegram ID.
func (uc *UserUseCase) GetUser(ctx context.Context, telegramID int64) (*user.User, error) {
	return uc.users.FindByTelegramID(ctx, telegramID)
}
