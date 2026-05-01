package hiddify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/pkg/apperr"
)

const (
	defaultTimeout = 10 * time.Second
)

type Client struct {
	baseURL    string
	adminProxy string
	apiKey     string
	http       *http.Client
	log        *slog.Logger
}

func NewClient(baseURL, adminProxy, apiKey string, log *slog.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		adminProxy: adminProxy,
		apiKey:     apiKey,
		http:       &http.Client{Timeout: defaultTimeout},
		log:        log.With("component", "hiddify_client"),
	}
}

type userInfoResponse struct {
	UUID            string  `json:"uuid"`
	Name            string  `json:"name"`
	TelegramID      *int64  `json:"telegram_id"`
	IsActive        bool    `json:"is_active"`
	UsedTrafficGB   float64 `json:"current_usage_GB"`
	TotalTrafficGB  float64 `json:"usage_limit_GB"` // 0 = unlimited
	PackageDays     int     `json:"package_days"`
	StartDate       string  `json:"start_date"` // "2024-01-15"
	SubscriptionURL string  `json:"subscription_url"`
}

type allUsersResponse []userInfoResponse

func (c *Client) GetUserByUUID(ctx context.Context, uuid string) (*subscription.Status, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/%s/", c.adminProxy, uuid)
	var resp userInfoResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return toStatus(resp, c.baseURL, uuid), nil
}

func (c *Client) GetUserByTelegramID(ctx context.Context, telegramID int64) (*subscription.Status, string, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/", c.adminProxy)
	var resp allUsersResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, "", err
	}

	for _, u := range resp {
		if u.TelegramID != nil && *u.TelegramID == telegramID {
			return toStatus(u, c.baseURL, u.UUID), u.UUID, nil
		}
	}
	return nil, "", apperr.ErrNotFound
}

func (c *Client) ListUsers(ctx context.Context) ([]subscription.PanelUser, error) {
	path := fmt.Sprintf("/%s/api/v2/admin/user/", c.adminProxy)
	var resp allUsersResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	users := make([]subscription.PanelUser, 0, len(resp))
	for _, u := range resp {
		users = append(users, subscription.PanelUser{
			UUID:       u.UUID,
			Name:       u.Name,
			TelegramID: u.TelegramID,
		})
	}
	return users, nil
}

func (c *Client) SetTelegramID(ctx context.Context, uuid string, telegramID int64) error {
	path := fmt.Sprintf("/%s/api/v2/admin/user/%s/", c.adminProxy, uuid)
	body := map[string]any{"telegram_id": telegramID}
	return c.patch(ctx, path, body)
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("hiddify get: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", apperr.ErrHiddifyAPI, err)
	}
	defer resp.Body.Close()

	return c.decode(resp, dest)
}

func (c *Client) patch(ctx context.Context, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("hiddify patch: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path,
		newJSONBody(data))
	if err != nil {
		return fmt.Errorf("hiddify patch: build request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", apperr.ErrHiddifyAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			c.log.Error("hiddify patch error", "status", resp.StatusCode, "read_body_err", readErr)
		} else {
			c.log.Error("hiddify patch error", "status", resp.StatusCode, "body", string(body))
		}
		return fmt.Errorf("%w: status %d", apperr.ErrHiddifyAPI, resp.StatusCode)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Hiddify-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
}

func (c *Client) decode(resp *http.Response, dest any) error {
	if resp.StatusCode == http.StatusNotFound {
		return apperr.ErrNotFound
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		c.log.Error("hiddify error response", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("%w: status %d", apperr.ErrHiddifyAPI, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("hiddify: decode response: %w", err)
	}
	return nil
}

func toStatus(r userInfoResponse, baseURL, uuid string) *subscription.Status {
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

	if s.SubscriptionURL == "" && baseURL != "" && uuid != "" {
		s.SubscriptionURL = fmt.Sprintf("%s/%s/", baseURL, uuid)
	}
	return s
}

func gbToBytes(gb float64) int64 {
	if gb == 0 {
		return 0
	}
	return int64(gb * 1024 * 1024 * 1024)
}

func newJSONBody(data []byte) io.Reader {
	return &bytesReader{data: data, pos: 0}
}

type bytesReader struct {
	data []byte
	pos  int
}

func (b *bytesReader) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
