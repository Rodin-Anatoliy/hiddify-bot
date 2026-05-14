// Package hiddify provides an HTTP client for the Hiddify Manager API v2.
package hiddify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
)

const defaultTimeout = 15 * time.Second

// Client communicates with the Hiddify panel REST API v2.
type Client struct {
	baseURL    string
	adminProxy string
	userProxy  string // path used in user-facing subscription URLs
	apiKey     string
	http       *http.Client
	log        *slog.Logger
}

// NewClient constructs a ready-to-use Hiddify API client.
func NewClient(baseURL, adminProxy, userProxy, apiKey string, log *slog.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		adminProxy: adminProxy,
		userProxy:  userProxy,
		apiKey:     apiKey,
		http:       &http.Client{Timeout: defaultTimeout},
		log:        log.With("component", "hiddify_client"),
	}
}

type apiUser struct {
	UUID            string  `json:"uuid"`
	Name            string  `json:"name"`
	TelegramID      *int64  `json:"telegram_id"`
	IsActive        bool    `json:"is_active"`
	UsedTrafficGB   float64 `json:"current_usage_GB"`
	TotalTrafficGB  float64 `json:"usage_limit_GB"` // 0 = unlimited
	PackageDays     int     `json:"package_days"`
	StartDate       string  `json:"start_date"` // "2006-01-02"
	SubscriptionURL string  `json:"subscription_url"`
}

// GetUserByUUID fetches a single Hiddify user and maps it to a subscription status.
func (c *Client) GetUserByUUID(ctx context.Context, uuid string) (*subscription.Status, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/%s/", c.adminProxy, uuid)
	var raw apiUser
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, err
	}
	return toStatus(raw, c.baseURL, c.userProxy, uuid), nil
}

// GetUserByTelegramID finds the first panel user whose telegram_id matches.
func (c *Client) GetUserByTelegramID(ctx context.Context, telegramID int64) (*subscription.Status, string, error) {
	all, err := c.listRaw(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, u := range all {
		if u.TelegramID != nil && *u.TelegramID == telegramID {
			return toStatus(u, c.baseURL, c.userProxy, u.UUID), u.UUID, nil
		}
	}
	return nil, "", domain.ErrNotFound
}

// SetTelegramID links a Telegram chat ID to an existing Hiddify user.
func (c *Client) SetTelegramID(ctx context.Context, uuid string, telegramID int64) error {
	path := fmt.Sprintf("/%s/api/v2/admin/user/%s/", c.adminProxy, uuid)
	return c.patch(ctx, path, map[string]any{"telegram_id": telegramID})
}

func (c *Client) listRaw(ctx context.Context) ([]apiUser, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/", c.adminProxy)
	var out []apiUser
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("hiddify get: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", domain.ErrHiddifyAPI, err)
	}
	defer resp.Body.Close()
	return c.decode(resp, dest)
}

func (c *Client) patch(ctx context.Context, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("hiddify patch: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("hiddify patch: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", domain.ErrHiddifyAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		c.log.Error("hiddify patch error", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("%w: status %d", domain.ErrHiddifyAPI, resp.StatusCode)
	}
	return nil
}

func (c *Client) decode(resp *http.Response, dest any) error {
	if resp.StatusCode == http.StatusNotFound {
		return domain.ErrNotFound
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		c.log.Error("hiddify error response", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("%w: status %d", domain.ErrHiddifyAPI, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("hiddify: decode: %w", err)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Hiddify-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
}

func toStatus(r apiUser, baseURL, userProxy, uuid string) *subscription.Status {
	s := &subscription.Status{
		UUID:              uuid,
		UsedTrafficBytes:  gbToBytes(r.UsedTrafficGB),
		TotalTrafficBytes: gbToBytes(r.TotalTrafficGB),
		IsActive:          r.IsActive,
		SubscriptionURL:   r.SubscriptionURL,
	}
	if r.StartDate != "" {
		if t, err := time.Parse("2006-01-02", r.StartDate); err == nil {
			s.StartDate = t
			if r.PackageDays > 0 {
				exp := t.AddDate(0, 0, r.PackageDays)
				s.ExpireDate = &exp
			}
		}
	}
	if s.SubscriptionURL == "" && uuid != "" {
		if userProxy != "" {
			s.SubscriptionURL = fmt.Sprintf("%s/%s/%s/", baseURL, userProxy, uuid)
		} else {
			s.SubscriptionURL = fmt.Sprintf("%s/%s/", baseURL, uuid)
		}
	}
	return s
}

func gbToBytes(gb float64) int64 {
	if gb == 0 {
		return 0
	}
	return int64(gb * 1024 * 1024 * 1024)
}
