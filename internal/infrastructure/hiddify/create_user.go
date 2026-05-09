package hiddify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/usecase"
)

// CreateUser creates a new user in the Hiddify panel.
// Implements usecase.HiddifyClient.
func (c *Client) CreateUser(ctx context.Context, req usecase.CreateUserRequest) (*usecase.CreatedUser, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/", c.adminProxy)

	payload := map[string]any{
		"name":            req.Name,
		"telegram_id":     req.TelegramID,
		"usage_limit_GB":  100000,
		"package_days":    10000,
		"mode":            "no_reset",
		"enable":          true,
		"lang":            "ru",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hiddify create user: marshal: %w", err)
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("hiddify create user: %w", err)
	}
	c.setHeaders(req2)
	req2.Header.Set("Content-Type", "application/json")

	resp, doErr := c.http.Do(req2)
	if doErr != nil {
		return nil, fmt.Errorf("hiddify create user: %w", doErr)
	}
	defer resp.Body.Close()

	var created apiUser
	if err := c.decode(resp, &created); err != nil {
		return nil, fmt.Errorf("hiddify create user: %w", err)
	}

	subURL := fmt.Sprintf("%s/%s/%s/", c.baseURL, c.userProxy, created.UUID)
	return &usecase.CreatedUser{
		UUID:            created.UUID,
		SubscriptionURL: subURL,
		ExpiresAt:       time.Now().AddDate(0, 0, 10000),
	}, nil
}
