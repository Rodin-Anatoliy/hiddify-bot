package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/user"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/port"
)

const (
	// broadcastWorkers limits concurrent Telegram sends to stay under API rate limits.
	broadcastWorkers = 5
	// broadcastDelay is the pause between sends per worker (~30 msg/s total across all workers).
	broadcastDelay = 50 * time.Millisecond
)

// BroadcastMessage is the payload for a broadcast — text only or text+photo.
type BroadcastMessage struct {
	Text   string
	FileID string // Telegram photo file_id; empty means text-only
}

// BroadcastResult carries aggregate delivery statistics.
type BroadcastResult struct {
	Total   int
	Success int32
	Failed  int32
}

// BroadcastUseCase sends a message to all users who have enabled messaging (/start).
type BroadcastUseCase struct {
	users  user.Repository
	sender port.Sender
	log    *slog.Logger
}

func NewBroadcastUseCase(users user.Repository, sender port.Sender, log *slog.Logger) *BroadcastUseCase {
	return &BroadcastUseCase{
		users:  users,
		sender: sender,
		log:    log.With("usecase", "broadcast"),
	}
}

// Send dispatches msg to every messageable linked user.
// Uses a bounded worker pool so we never spawn O(n) goroutines.
func (uc *BroadcastUseCase) Send(ctx context.Context, msg BroadcastMessage) (*BroadcastResult, error) {
	recipients, err := uc.users.FindAllLinked(ctx)
	if err != nil {
		return nil, fmt.Errorf("broadcast: load recipients: %w", err)
	}

	result := &BroadcastResult{Total: len(recipients)}
	if result.Total == 0 {
		uc.log.Info("broadcast: no recipients, skipping")
		return result, nil
	}

	jobs := make(chan *user.User, len(recipients))
	for _, u := range recipients {
		jobs <- u
	}
	close(jobs)

	g, gCtx := errgroup.WithContext(ctx)
	for range broadcastWorkers {
		g.Go(func() error {
			for u := range jobs {
				if gCtx.Err() != nil {
					return gCtx.Err()
				}
				if err := uc.deliver(gCtx, u.TelegramID, msg); err != nil {
					atomic.AddInt32(&result.Failed, 1)
					uc.log.Warn("broadcast: delivery failed", "telegram_id", u.TelegramID, "err", err)
				} else {
					atomic.AddInt32(&result.Success, 1)
				}
				time.Sleep(broadcastDelay)
			}
			return nil
		})
	}

	// We don't propagate the errgroup error — it's only context cancellation.
	// Partial results are still useful.
	_ = g.Wait()

	uc.log.Info("broadcast done",
		"total", result.Total, "ok", result.Success, "fail", result.Failed)
	return result, nil
}

func (uc *BroadcastUseCase) deliver(ctx context.Context, telegramID int64, msg BroadcastMessage) error {
	if msg.FileID != "" {
		return uc.sender.SendPhoto(ctx, telegramID, msg.FileID, msg.Text)
	}
	return uc.sender.SendText(ctx, telegramID, msg.Text)
}
