package usecase

import "context"

// Sender delivers messages to Telegram users.
type Sender interface {
	SendText(ctx context.Context, telegramID int64, text string) error
	SendPhoto(ctx context.Context, telegramID int64, fileID, caption string) error
}
