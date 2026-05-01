package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
)

type HiddifyClient interface {
	GetUserByUUID(ctx context.Context, uuid string) (*subscription.Status, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*subscription.Status, string, error)
	ListUsers(ctx context.Context) ([]subscription.PanelUser, error)
	SetTelegramID(ctx context.Context, uuid string, telegramID int64) error
}

type UserUseCase struct {
	users   user.Repository
	hiddify HiddifyClient
	log     *slog.Logger
}

func NewUserUseCase(users user.Repository, hiddify HiddifyClient, log *slog.Logger) *UserUseCase {
	return &UserUseCase{users: users, hiddify: hiddify, log: log.With("usecase", "user")}
}

func (uc *UserUseCase) RegisterOrGet(ctx context.Context, telegramID int64, username string) (*user.User, error) {
	now := time.Now()
	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if err == nil {
		u.Username = username
		u.CanMessage = true
		u.LastSeen = &now
		if !u.IsLinked() {
			uc.tryAutoLink(ctx, u, now)
		}
		if err := uc.users.Save(ctx, u); err != nil {
			return nil, fmt.Errorf("register: update: %w", err)
		}
		return u, nil
	}
	if !apperr.Is(err, apperr.ErrNotFound) {
		return nil, fmt.Errorf("register: lookup: %w", err)
	}

	u = &user.User{
		TelegramID: telegramID,
		Username:   username,
		CanMessage: true,
		LastSeen:   &now,
		CreatedAt:  now,
	}

	uc.tryAutoLink(ctx, u, now)

	if err := uc.users.Save(ctx, u); err != nil {
		return nil, fmt.Errorf("register: save: %w", err)
	}
	return u, nil
}

func (uc *UserUseCase) tryAutoLink(ctx context.Context, u *user.User, now time.Time) {
	_, uuid, linkErr := uc.hiddify.GetUserByTelegramID(ctx, u.TelegramID)
	if linkErr == nil {
		u.HiddifyUUID = uuid
		u.LinkSource = "start_auto"
		u.LinkedAt = &now
		uc.log.Info("auto-linked hiddify user", "telegram_id", u.TelegramID, "uuid", uuid)
	}
}

func (uc *UserUseCase) LinkManually(ctx context.Context, telegramID int64, uuid string) error {
	if _, err := uc.hiddify.GetUserByUUID(ctx, uuid); err != nil {
		return fmt.Errorf("link: validate uuid: %w", err)
	}

	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if apperr.Is(err, apperr.ErrNotFound) {
		u = &user.User{
			TelegramID: telegramID,
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

	if err := uc.hiddify.SetTelegramID(ctx, uuid, telegramID); err != nil {
		uc.log.Warn("could not set telegram_id in hiddify", "err", err)
	}
	return nil
}

type SyncResult struct {
	Total   int
	Linked  int
	Created int
	Updated int
	Skipped int
}

func (uc *UserUseCase) SyncFromHiddify(ctx context.Context) (*SyncResult, error) {
	panelUsers, err := uc.hiddify.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("sync: list users: %w", err)
	}

	result := &SyncResult{Total: len(panelUsers)}
	now := time.Now()

	for _, panelUser := range panelUsers {
		if panelUser.TelegramID == nil || *panelUser.TelegramID == 0 {
			result.Skipped++
			continue
		}

		result.Linked++
		telegramID := *panelUser.TelegramID
		u, err := uc.users.FindByTelegramID(ctx, telegramID)
		if apperr.Is(err, apperr.ErrNotFound) {
			u = &user.User{
				TelegramID:  telegramID,
				HiddifyUUID: panelUser.UUID,
				Username:    panelUser.Name,
				LinkSource:  "hiddify_sync",
				LinkedAt:    &now,
				CreatedAt:   now,
				CanMessage:  false,
			}
			if err := uc.users.Save(ctx, u); err != nil {
				return nil, fmt.Errorf("sync: create user: %w", err)
			}
			result.Created++
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("sync: find user: %w", err)
		}

		changed := false
		if u.HiddifyUUID != panelUser.UUID {
			u.HiddifyUUID = panelUser.UUID
			u.LinkedAt = &now
			changed = true
		}
		if u.LinkSource == "" || u.LinkSource == "start_auto" {
			u.LinkSource = "hiddify_sync"
			changed = true
		}
		if u.Username == "" && panelUser.Name != "" {
			u.Username = panelUser.Name
			changed = true
		}
		if changed {
			if err := uc.users.Save(ctx, u); err != nil {
				return nil, fmt.Errorf("sync: update user: %w", err)
			}
			result.Updated++
		}
	}

	return result, nil
}

func (uc *UserUseCase) GetSubscription(ctx context.Context, telegramID int64) (*subscription.Status, error) {
	u, err := uc.users.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if !u.IsLinked() {
		return nil, apperr.ErrNotFound
	}
	return uc.hiddify.GetUserByUUID(ctx, u.HiddifyUUID)
}
