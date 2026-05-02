package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/port"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockUserRepo struct {
	data map[int64]*user.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{data: make(map[int64]*user.User)}
}

func (m *mockUserRepo) Save(_ context.Context, u *user.User) error {
	cp := *u
	m.data[u.TelegramID] = &cp
	return nil
}

func (m *mockUserRepo) FindByTelegramID(_ context.Context, id int64) (*user.User, error) {
	u, ok := m.data[id]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *mockUserRepo) FindByHiddifyUUID(_ context.Context, uuid string) (*user.User, error) {
	for _, u := range m.data {
		if u.HiddifyUUID == uuid {
			cp := *u
			return &cp, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (m *mockUserRepo) FindAllLinked(_ context.Context) ([]*user.User, error) {
	var result []*user.User
	for _, u := range m.data {
		if u.IsLinked() && u.CanMessage {
			cp := *u
			result = append(result, &cp)
		}
	}
	return result, nil
}

type mockHiddify struct {
	byTelegram map[int64]string
}

func newMockHiddify(pairs map[int64]string) *mockHiddify {
	if pairs == nil {
		pairs = make(map[int64]string)
	}
	return &mockHiddify{byTelegram: pairs}
}

func (m *mockHiddify) GetUserByUUID(_ context.Context, uuid string) (*subscription.Status, error) {
	for _, u := range m.byTelegram {
		if u == uuid {
			return &subscription.Status{UUID: uuid, IsActive: true}, nil
		}
	}
	return nil, apperr.ErrNotFound
}

func (m *mockHiddify) GetUserByTelegramID(_ context.Context, telegramID int64) (*subscription.Status, string, error) {
	uuid, ok := m.byTelegram[telegramID]
	if !ok {
		return nil, "", apperr.ErrNotFound
	}
	return &subscription.Status{UUID: uuid, IsActive: true}, uuid, nil
}

func (m *mockHiddify) ListPanelUsers(_ context.Context) ([]usecase.PanelUserDTO, error) {
	out := make([]usecase.PanelUserDTO, 0, len(m.byTelegram))
	for tgID, uuid := range m.byTelegram {
		id := tgID
		out = append(out, usecase.PanelUserDTO{UUID: uuid, TelegramID: &id})
	}
	return out, nil
}

func (m *mockHiddify) SetTelegramID(_ context.Context, _ string, _ int64) error { return nil }

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestRegisterOrGet_NewUser_AutoLinked(t *testing.T) {
	repo := newMockUserRepo()
	uc := usecase.NewUserUseCase(repo, newMockHiddify(map[int64]string{42: "uuid-abc"}), logger.New("debug"))

	u, err := uc.RegisterOrGet(context.Background(), 42, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !u.IsLinked() {
		t.Error("expected auto-link")
	}
	if u.HiddifyUUID != "uuid-abc" {
		t.Errorf("wrong uuid: %s", u.HiddifyUUID)
	}
	if !u.CanMessage {
		t.Error("CanMessage should be true after /start")
	}
	if u.LinkSource != "auto" {
		t.Errorf("expected link_source=auto, got %s", u.LinkSource)
	}
}

func TestRegisterOrGet_ExistingUser_UpdatesLastSeen(t *testing.T) {
	repo := newMockUserRepo()
	now := time.Now().Add(-time.Hour)
	_ = repo.Save(context.Background(), &user.User{
		TelegramID:  99,
		HiddifyUUID: "existing-uuid",
		LinkedAt:    &now,
		CreatedAt:   now,
	})

	uc := usecase.NewUserUseCase(repo, newMockHiddify(nil), logger.New("debug"))

	u, err := uc.RegisterOrGet(context.Background(), 99, "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.HiddifyUUID != "existing-uuid" {
		t.Error("should keep existing uuid")
	}
	if !u.CanMessage {
		t.Error("CanMessage should be true after /start")
	}
	if u.LastSeen == nil {
		t.Error("LastSeen should be set")
	}
}

func TestLinkManually_ExistingUser(t *testing.T) {
	repo := newMockUserRepo()
	_ = repo.Save(context.Background(), &user.User{TelegramID: 77, CreatedAt: time.Now()})

	uc := usecase.NewUserUseCase(repo, newMockHiddify(map[int64]string{1: "target-uuid"}), logger.New("debug"))

	if err := uc.LinkManually(context.Background(), 77, "target-uuid"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := repo.FindByTelegramID(context.Background(), 77)
	if u.HiddifyUUID != "target-uuid" {
		t.Errorf("uuid not set: %s", u.HiddifyUUID)
	}
	if u.LinkSource != "admin" {
		t.Errorf("expected link_source=admin, got %s", u.LinkSource)
	}
}

func TestLinkManually_CreatesLocalUser_NotMessageable(t *testing.T) {
	repo := newMockUserRepo()
	uc := usecase.NewUserUseCase(repo, newMockHiddify(map[int64]string{1: "target-uuid"}), logger.New("debug"))

	// User 77 has never done /start
	if err := uc.LinkManually(context.Background(), 77, "target-uuid"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, err := repo.FindByTelegramID(context.Background(), 77)
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if u.CanMessage {
		t.Error("user linked without /start should not be messageable")
	}
}

func TestLinkManually_InvalidUUID(t *testing.T) {
	repo := newMockUserRepo()
	uc := usecase.NewUserUseCase(repo, newMockHiddify(nil), logger.New("debug"))

	err := uc.LinkManually(context.Background(), 77, "nonexistent-uuid")
	if err == nil {
		t.Fatal("expected error for invalid uuid")
	}
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestSyncFromHiddify_CreatesNonMessageable(t *testing.T) {
	repo := newMockUserRepo()
	uc := usecase.NewUserUseCase(repo, newMockHiddify(map[int64]string{42: "uuid-abc"}), logger.New("debug"))

	result, err := uc.SyncFromHiddify(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("expected 1 created, got %+v", result)
	}

	u, err := repo.FindByTelegramID(context.Background(), 42)
	if err != nil {
		t.Fatalf("synced user not in DB: %v", err)
	}
	if u.CanMessage {
		t.Error("synced user should not be messageable before /start")
	}
	if u.LinkSource != "sync" {
		t.Errorf("expected link_source=sync, got %s", u.LinkSource)
	}
}
