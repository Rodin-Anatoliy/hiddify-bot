package hiddify

import (
	"context"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
)

func (c *Client) ListPanelUsers(ctx context.Context) ([]subscription.PanelUser, error) {
	raw, err := c.listRaw(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]subscription.PanelUser, 0, len(raw))
	for _, u := range raw {
		out = append(out, subscription.PanelUser{
			UUID:       u.UUID,
			Name:       u.Name,
			TelegramID: u.TelegramID,
		})
	}
	return out, nil
}

var _ subscription.Repository = (*Client)(nil)
