package hiddify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
)

// CreateUser creates a new user in the Hiddify panel.
// Implements subscription.Repository.
func (c *Client) CreateUser(ctx context.Context, req subscription.CreateUserRequest) (*subscription.CreatedUser, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/", c.adminProxy)

	usageLimitGB, packageDays, mode := c.createDefaults.UsageLimitGB, c.createDefaults.PackageDays, c.createDefaults.Mode
	if req.UsageLimitGB != 0 {
		usageLimitGB = req.UsageLimitGB
	}
	if req.PackageDays != 0 {
		packageDays = req.PackageDays
	}
	if req.Mode != "" {
		mode = req.Mode
	}

	payload := map[string]any{
		"name":           req.Name,
		"telegram_id":    req.TelegramID,
		"usage_limit_GB": usageLimitGB,
		"package_days":   packageDays,
		"mode":           mode,
		"enable":         c.createDefaults.Enable,
		"lang":           c.createDefaults.Lang,
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

	if created.UUID == "" {
		return nil, fmt.Errorf("hiddify create user: %w: empty uuid in response", domain.ErrHiddifyAPI)
	}

	subURL := fmt.Sprintf("%s/%s/%s/", c.baseURL, c.userProxy, created.UUID)
	return &subscription.CreatedUser{
		UUID:            created.UUID,
		SubscriptionURL: subURL,
		ExpiresAt:       time.Now().AddDate(0, 0, packageDays),
	}, nil
}
