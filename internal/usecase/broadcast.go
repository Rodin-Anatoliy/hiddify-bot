package usecase

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"
)

const (
	broadcastWorkers = 5
	broadcastDelay   = 50 * time.Millisecond
)

type BroadcastMessage struct {
	Text   string
	FileID string
}

type BroadcastResult struct {
	Total   int
	Success int32
	Failed  int32
}

type Sender interface {
	SendText(ctx context.Context, telegramID int64, text string) error
	SendPhoto(ctx context.Context, telegramID int64, fileID, caption string) error
}

type BroadcastUseCase struct {
	users  user.Repository
	sender Sender
	log    *slog.Logger
}

func NewBroadcastUseCase(users user.Repository, sender Sender, log *slog.Logger) *BroadcastUseCase {
	return &BroadcastUseCase{users: users, sender: sender, log: log.With("usecase", "broadcast")}
}

func (uc *BroadcastUseCase) Send(ctx context.Context, msg BroadcastMessage) (*BroadcastResult, error) {
	recipients, err := uc.users.FindAllLinked(ctx)
	if err != nil {
		return nil, err
	}

	result := &BroadcastResult{Total: len(recipients)}
	if result.Total == 0 {
		uc.log.Info("broadcast: no linked users, skipping")
		return result, nil
	}

	jobs := make(chan *user.User, len(recipients))
	for _, u := range recipients {
		jobs <- u
	}
	close(jobs)

	g, gCtx := errgroup.WithContext(ctx)
	for i := 0; i < broadcastWorkers; i++ {
		g.Go(func() error {
			for u := range jobs {
				if gCtx.Err() != nil {
					return gCtx.Err()
				}
				if err := uc.deliver(gCtx, u.TelegramID, msg); err != nil {
					atomic.AddInt32(&result.Failed, 1)
					uc.log.Warn("broadcast: delivery failed",
						"telegram_id", u.TelegramID, "err", err)
				} else {
					atomic.AddInt32(&result.Success, 1)
				}
				time.Sleep(broadcastDelay)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		uc.log.Warn("broadcast: some workers encountered errors", "err", err)
	}

	uc.log.Info("broadcast completed",
		"total", result.Total, "ok", result.Success, "fail", result.Failed)
	return result, nil
}

func (uc *BroadcastUseCase) deliver(ctx context.Context, telegramID int64, msg BroadcastMessage) error {
	if msg.FileID != "" {
		return uc.sender.SendPhoto(ctx, telegramID, msg.FileID, msg.Text)
	}
	return uc.sender.SendText(ctx, telegramID, msg.Text)
}
