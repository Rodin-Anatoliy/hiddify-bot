package tg

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitBroadcastTurnAllowsDeliveryOnTick(t *testing.T) {
	t.Parallel()

	ticks := make(chan time.Time, 1)
	ticks <- time.Now()

	if err := waitBroadcastTurn(context.Background(), ticks); err != nil {
		t.Fatalf("waitBroadcastTurn() error = %v", err)
	}
}

func TestWaitBroadcastTurnStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitBroadcastTurn(ctx, make(chan time.Time))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitBroadcastTurn() error = %v, want context.Canceled", err)
	}
}
