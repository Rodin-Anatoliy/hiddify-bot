package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/logger"
)

type mockUserRepo struct {
	data map[int64]*user.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{data: make(map[int64]*user.User)}
}
func (m *mockUserRepo) Save(_ context.Context, u *user.User) error {
	m.data[u.TelegramID] = u
	return nil
}
func (m *mockUserRepo) FindByTelegramID(_ context.Context, id int64) (*user.User, error) {
	u, ok := m.data[id]
	if !ok {
		return nil, apperr.ErrNotFound
	}
	return u, nil
}
func (m *mockUserRepo) FindByHiddifyUUID(_ context.Context, uuid string) (*user.User, error) {
	for _, u := range m.data {
		if u.HiddifyUUID == uuid {
			return u, nil
		}
	}
	return nil, apperr.ErrNotFound
}
func (m *mockUserRepo) FindAllLinked(_ context.Context) ([]*user.User, error) {
	var result []*user.User
	for _, u := range m.data {
		if u.IsLinked() && u.CanMessage {
			result = append(result, u)
		}
	}
	return result, nil
}

type mockHiddify struct {
	users map[int64]string
}

func newMockHiddify(pairs map[int64]string) *mockHiddify {
	return &mockHiddify{users: pairs}
}
func (m *mockHiddify) GetUserByUUID(_ context.Context, uuid string) (*subscription.Status, error) {
	for _, u := range m.users {
		if u == uuid {
			return &subscription.Status{UUID: uuid, IsActive: true}, nil
		}
	}
	return nil, apperr.ErrNotFound
}
func (m *mockHiddify) GetUserByTelegramID(_ context.Context, telegramID int64) (*subscription.Status, string, error) {
	uuid, ok := m.users[telegramID]
	if !ok {
		return nil, "", apperr.ErrNotFound
	}
	return &subscription.Status{UUID: uuid, IsActive: true}, uuid, nil
}
func (m *mockHiddify) ListUsers(_ context.Context) ([]subscription.PanelUser, error) {
	var users []subscription.PanelUser
	for telegramID, uuid := range m.users {
		id := telegramID
		users = append(users, subscription.PanelUser{
			UUID:       uuid,
			TelegramID: &id,
		})
	}
	return users, nil
}
func (m *mockHiddify) SetTelegramID(_ context.Context, _ string, _ int64) error { return nil }

func TestRegisterOrGet_NewUserAutoLinked(t *testing.T) {
	repo := newMockUserRepo()
	hiddify := newMockHiddify(map[int64]string{42: "uuid-abc"})
	log := logger.New("debug")

	uc := usecase.NewUserUseCase(repo, hiddify, log)

	u, err := uc.RegisterOrGet(context.Background(), 42, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !u.IsLinked() {
		t.Error("expected user to be auto-linked")
	}
	if u.HiddifyUUID != "uuid-abc" {
		t.Errorf("wrong uuid: %s", u.HiddifyUUID)
	}
	if !u.CanMessage {
		t.Error("expected user to be messageable after /start")
	}
}

func TestRegisterOrGet_ExistingUserReturned(t *testing.T) {
	repo := newMockUserRepo()
	now := time.Now()
	existing := &user.User{TelegramID: 99, HiddifyUUID: "existing-uuid", LinkedAt: &now, CreatedAt: now}
	_ = repo.Save(context.Background(), existing)

	hiddify := newMockHiddify(nil)
	log := logger.New("debug")
	uc := usecase.NewUserUseCase(repo, hiddify, log)

	u, err := uc.RegisterOrGet(context.Background(), 99, "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.HiddifyUUID != "existing-uuid" {
		t.Error("should return existing user unchanged")
	}
}

func TestLinkManually_BindsUUID(t *testing.T) {
	repo := newMockUserRepo()
	_ = repo.Save(context.Background(), &user.User{TelegramID: 77, CreatedAt: time.Now()})

	hiddify := newMockHiddify(map[int64]string{1: "target-uuid"})
	log := logger.New("debug")
	uc := usecase.NewUserUseCase(repo, hiddify, log)

	if err := uc.LinkManually(context.Background(), 77, "target-uuid"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := repo.FindByTelegramID(context.Background(), 77)
	if u.HiddifyUUID != "target-uuid" {
		t.Errorf("uuid not set: %s", u.HiddifyUUID)
	}
}

func TestLinkManually_CreatesMissingLocalUser(t *testing.T) {
	repo := newMockUserRepo()
	hiddify := newMockHiddify(map[int64]string{1: "target-uuid"})
	log := logger.New("debug")
	uc := usecase.NewUserUseCase(repo, hiddify, log)

	if err := uc.LinkManually(context.Background(), 77, "target-uuid"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, err := repo.FindByTelegramID(context.Background(), 77)
	if err != nil {
		t.Fatalf("expected local user: %v", err)
	}
	if u.HiddifyUUID != "target-uuid" {
		t.Errorf("uuid not set: %s", u.HiddifyUUID)
	}
	if u.CanMessage {
		t.Error("missing local user should not be messageable before /start")
	}
}

func TestSyncFromHiddify_CreatesNonMessageableLinks(t *testing.T) {
	repo := newMockUserRepo()
	hiddify := newMockHiddify(map[int64]string{42: "uuid-abc"})
	log := logger.New("debug")
	uc := usecase.NewUserUseCase(repo, hiddify, log)

	result, err := uc.SyncFromHiddify(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("expected one created user, got %+v", result)
	}
	u, err := repo.FindByTelegramID(context.Background(), 42)
	if err != nil {
		t.Fatalf("expected user after sync: %v", err)
	}
	if u.CanMessage {
		t.Error("synced user should wait for /start before broadcast")
	}
}
