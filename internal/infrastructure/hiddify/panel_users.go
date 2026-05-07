package hiddify

import (
	"context"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

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

var _ usecase.HiddifyClient = (*Client)(nil)
