package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/admin"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/ticket"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/sqlite"
)

func openTestDB(t *testing.T) *sqlite.DB {
	t.Helper()

	db, err := sqlite.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close test db: %v", err)
		}
	})
	return db
}

func TestUserRepository_SaveFindAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewUserRepository(openTestDB(t))

	linkedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	lastSeen := time.Now().UTC().Truncate(time.Second)
	createdAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)

	err := repo.Save(ctx, &user.User{
		TelegramID:  42,
		HiddifyUUID: "uuid-1",
		Username:    "alice",
		CanMessage:  true,
		LinkSource:  "auto",
		LinkedAt:    &linkedAt,
		LastSeen:    &lastSeen,
		CreatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("save user: %v", err)
	}

	got, err := repo.FindByTelegramID(ctx, 42)
	if err != nil {
		t.Fatalf("find by telegram id: %v", err)
	}
	if got.TelegramID != 42 || got.HiddifyUUID != "uuid-1" || got.Username != "alice" {
		t.Fatalf("unexpected user: %+v", got)
	}
	if !got.CanMessage || got.LinkSource != "auto" || got.LinkedAt == nil || got.LastSeen == nil {
		t.Fatalf("user fields not restored: %+v", got)
	}

	gotByUUID, err := repo.FindByHiddifyUUID(ctx, "uuid-1")
	if err != nil {
		t.Fatalf("find by hiddify uuid: %v", err)
	}
	if gotByUUID.TelegramID != 42 {
		t.Fatalf("wrong user by uuid: %+v", gotByUUID)
	}

	if err := repo.SetCanMessage(ctx, 42, false); err != nil {
		t.Fatalf("set can message: %v", err)
	}
	got, err = repo.FindByTelegramID(ctx, 42)
	if err != nil {
		t.Fatalf("find after set can message: %v", err)
	}
	if got.CanMessage {
		t.Fatal("expected CanMessage=false after update")
	}
}

func TestUserRepository_FindAllLinkedFiltersMessageableUsers(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewUserRepository(openTestDB(t))
	now := time.Now().UTC().Truncate(time.Second)

	users := []*user.User{
		{TelegramID: 1, HiddifyUUID: "uuid-messageable", CanMessage: true, CreatedAt: now},
		{TelegramID: 2, HiddifyUUID: "uuid-blocked", CanMessage: false, CreatedAt: now},
		{TelegramID: 3, HiddifyUUID: "", CanMessage: true, CreatedAt: now},
	}
	for _, u := range users {
		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("save user %d: %v", u.TelegramID, err)
		}
	}

	linked, err := repo.FindAllLinked(ctx)
	if err != nil {
		t.Fatalf("find linked: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 messageable linked user, got %d: %+v", len(linked), linked)
	}
	if linked[0].TelegramID != 1 {
		t.Fatalf("unexpected linked user: %+v", linked[0])
	}

	allWithUUID, err := repo.FindAllWithUUID(ctx)
	if err != nil {
		t.Fatalf("find all with uuid: %v", err)
	}
	if len(allWithUUID) != 2 {
		t.Fatalf("expected 2 users with uuid, got %d", len(allWithUUID))
	}
}

func TestUserRepository_NotFoundUsesDomainError(t *testing.T) {
	repo := sqlite.NewUserRepository(openTestDB(t))

	_, err := repo.FindByTelegramID(context.Background(), 404)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected domain.ErrNotFound, got %v", err)
	}
}

func TestTicketRepository_SaveAndListByTelegramID(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewTicketRepository(openTestDB(t))
	now := time.Now().UTC().Truncate(time.Second)

	messages := []*ticket.Message{
		{
			TelegramID:   42,
			Direction:    ticket.DirectionUserToAdmin,
			Text:         "help",
			AttachmentID: "photo-1",
			CreatedAt:    now,
		},
		{
			TelegramID: 43,
			Direction:  ticket.DirectionUserToAdmin,
			Text:       "other",
			CreatedAt:  now.Add(time.Second),
		},
		{
			TelegramID: 42,
			Direction:  ticket.DirectionAdminToUser,
			Text:       "answer",
			CreatedAt:  now.Add(2 * time.Second),
		},
	}

	for _, msg := range messages {
		if err := repo.Save(ctx, msg); err != nil {
			t.Fatalf("save ticket message: %v", err)
		}
	}

	got, err := repo.FindByTelegramID(ctx, 42)
	if err != nil {
		t.Fatalf("find ticket messages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Direction != ticket.DirectionUserToAdmin || got[0].AttachmentID != "photo-1" {
		t.Fatalf("unexpected first message: %+v", got[0])
	}
	if got[1].Direction != ticket.DirectionAdminToUser || got[1].Text != "answer" {
		t.Fatalf("unexpected second message: %+v", got[1])
	}
}

func TestAdminSessionRepository_SaveGetDeleteAndCleanup(t *testing.T) {
	ctx := context.Background()
	repo := sqlite.NewAdminSessionRepository(openTestDB(t))

	future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if err := repo.Save(ctx, admin.Session{
		MessageID:  100,
		TargetTgID: 42,
		ExpiresAt:  future,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	got, err := repo.Get(ctx, 100)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.TargetTgID != 42 || got.MessageID != 100 {
		t.Fatalf("unexpected session: %+v", got)
	}

	if err := repo.Delete(ctx, 100); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	_, err = repo.Get(ctx, 100)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected domain.ErrNotFound after delete, got %v", err)
	}

	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if err := repo.Save(ctx, admin.Session{
		MessageID:  101,
		TargetTgID: 43,
		ExpiresAt:  past,
	}); err != nil {
		t.Fatalf("save expired session: %v", err)
	}
	deleted, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted expired session, got %d", deleted)
	}
}
