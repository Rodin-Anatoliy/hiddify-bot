// Package port defines the outbound interfaces (ports) of the application.
// These are the contracts that infrastructure must implement.
// Separating ports from domain keeps domain free of delivery concerns.
package port

import "context"

// Sender is the outbound port for delivering messages to Telegram users.
// Implemented by infrastructure/telegram.Bot.
type Sender interface {
	SendText(ctx context.Context, telegramID int64, text string) error
	SendPhoto(ctx context.Context, telegramID int64, fileID, caption string) error
}
