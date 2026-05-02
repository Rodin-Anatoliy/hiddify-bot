package hiddify

import (
	"context"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

// ListPanelUsers implements usecase.HiddifyClient.
//
// Infrastructure importing usecase is correct here — this is Dependency Inversion:
// the interface (HiddifyClient) is owned by the usecase layer,
// the concrete implementation lives in infrastructure.
// usecase never imports infrastructure, so the rule is preserved.
func (c *Client) ListPanelUsers(ctx context.Context) ([]usecase.PanelUserDTO, error) {
	raw, err := c.listRaw(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]usecase.PanelUserDTO, 0, len(raw))
	for _, u := range raw {
		out = append(out, usecase.PanelUserDTO{
			UUID:       u.UUID,
			Name:       u.Name,
			TelegramID: u.TelegramID,
		})
	}
	return out, nil
}

// Compile-time check: *Client must implement usecase.HiddifyClient.
var _ usecase.HiddifyClient = (*Client)(nil)
