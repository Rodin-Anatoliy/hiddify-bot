package hiddify_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/repository/hiddify"
)

const (
	testAdminProxy = "admin"
	testUserProxy  = "sub"
	testAPIKey     = "secret-key"
)

func TestClientGetUserByUUIDMapsStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/admin/api/v2/admin/user/uuid-1/")
		respondJSON(t, w, map[string]any{
			"uuid":             "uuid-1",
			"name":             "Alice",
			"is_active":        true,
			"current_usage_GB": 1.5,
			"usage_limit_GB":   10,
			"package_days":     30,
			"start_date":       "2026-01-01",
			"subscription_url": "",
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	status, err := client.GetUserByUUID(context.Background(), "uuid-1")
	if err != nil {
		t.Fatalf("GetUserByUUID() error = %v", err)
	}

	assertStatus(t, status, server.URL, "uuid-1")
}

func TestClientGetUserByTelegramIDFindsMappedUser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/admin/api/v2/admin/user/")
		respondJSON(t, w, []map[string]any{
			{
				"uuid":        "other",
				"name":        "Other",
				"telegram_id": int64(100),
				"is_active":   false,
			},
			{
				"uuid":             "uuid-1",
				"name":             "Alice",
				"telegram_id":      int64(42),
				"is_active":        true,
				"current_usage_GB": 1.5,
				"usage_limit_GB":   10,
				"package_days":     30,
				"start_date":       "2026-01-01",
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	status, uuid, err := client.GetUserByTelegramID(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetUserByTelegramID() error = %v", err)
	}

	if uuid != "uuid-1" {
		t.Fatalf("uuid = %q, want uuid-1", uuid)
	}
	assertStatus(t, status, server.URL, "uuid-1")
}

func TestClientGetUserByTelegramIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/admin/api/v2/admin/user/")
		respondJSON(t, w, []map[string]any{
			{"uuid": "uuid-1", "telegram_id": int64(41)},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	_, _, err := client.GetUserByTelegramID(context.Background(), 42)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetUserByTelegramID() error = %v, want ErrNotFound", err)
	}
}

func TestClientListPanelUsersMapsUsers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/admin/api/v2/admin/user/")
		respondJSON(t, w, []map[string]any{
			{"uuid": "uuid-1", "name": "Alice", "telegram_id": int64(42)},
			{"uuid": "uuid-2", "name": "Bob", "telegram_id": nil},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	users, err := client.ListPanelUsers(context.Background())
	if err != nil {
		t.Fatalf("ListPanelUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].UUID != "uuid-1" || users[0].Name != "Alice" || users[0].TelegramID == nil || *users[0].TelegramID != 42 {
		t.Fatalf("users[0] = %+v, want mapped user with telegram id", users[0])
	}
	if users[1].UUID != "uuid-2" || users[1].Name != "Bob" || users[1].TelegramID != nil {
		t.Fatalf("users[1] = %+v, want mapped user without telegram id", users[1])
	}
}

func TestClientSetTelegramIDSendsPatchPayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPatch, "/admin/api/v2/admin/user/uuid-1/")
		assertJSONContentType(t, r)

		var payload map[string]int64
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["telegram_id"] != 42 {
			t.Fatalf("telegram_id = %d, want 42", payload["telegram_id"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	if err := client.SetTelegramID(context.Background(), "uuid-1", 42); err != nil {
		t.Fatalf("SetTelegramID() error = %v", err)
	}
}

func TestClientCreateUserSendsDefaultsAndMapsCreatedUser(t *testing.T) {
	t.Parallel()

	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/admin/api/v2/admin/user/")
		assertJSONContentType(t, r)

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		assertPayloadValue(t, payload, "name", "Alice")
		assertPayloadValue(t, payload, "telegram_id", float64(42))
		assertPayloadValue(t, payload, "usage_limit_GB", float64(100000))
		assertPayloadValue(t, payload, "package_days", float64(10000))
		assertPayloadValue(t, payload, "mode", "no_reset")
		assertPayloadValue(t, payload, "enable", true)
		assertPayloadValue(t, payload, "lang", "ru")

		respondJSON(t, w, map[string]any{"uuid": "uuid-1"})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	created, err := client.CreateUser(context.Background(), subscription.CreateUserRequest{
		Name:       "Alice",
		TelegramID: 42,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if created.UUID != "uuid-1" {
		t.Fatalf("UUID = %q, want uuid-1", created.UUID)
	}
	wantURL := server.URL + "/sub/uuid-1/"
	if created.SubscriptionURL != wantURL {
		t.Fatalf("SubscriptionURL = %q, want %q", created.SubscriptionURL, wantURL)
	}
	if created.ExpiresAt.Before(start.AddDate(0, 0, 9999)) {
		t.Fatalf("ExpiresAt = %v, want about 10000 days from now", created.ExpiresAt)
	}
}

func TestClientMapsHTTPErrorSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		want       error
	}{
		{name: "not found", statusCode: http.StatusNotFound, want: domain.ErrNotFound},
		{name: "server error", statusCode: http.StatusInternalServerError, want: domain.ErrHiddifyAPI},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertRequest(t, r, http.MethodGet, "/admin/api/v2/admin/user/uuid-1/")
				http.Error(w, "panel failed", tt.statusCode)
			}))
			defer server.Close()

			client := newTestClient(server.URL)
			_, err := client.GetUserByUUID(context.Background(), "uuid-1")
			if !errors.Is(err, tt.want) {
				t.Fatalf("GetUserByUUID() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func newTestClient(baseURL string) *hiddify.Client {
	return hiddify.NewClient(baseURL, testAdminProxy, testUserProxy, testAPIKey, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func assertRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()

	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
	if r.URL.Path != path {
		t.Fatalf("path = %s, want %s", r.URL.Path, path)
	}
	if got := r.Header.Get("Hiddify-API-Key"); got != testAPIKey {
		t.Fatalf("Hiddify-API-Key = %q, want %q", got, testAPIKey)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
}

func assertJSONContentType(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func assertStatus(t *testing.T, status *subscription.Status, baseURL, uuid string) {
	t.Helper()

	if status.UUID != uuid {
		t.Fatalf("UUID = %q, want %q", status.UUID, uuid)
	}
	if !status.IsActive {
		t.Fatal("IsActive = false, want true")
	}
	if status.UsedTrafficBytes != int64(1.5*1024*1024*1024) {
		t.Fatalf("UsedTrafficBytes = %d, want 1.5 GiB", status.UsedTrafficBytes)
	}
	if status.TotalTrafficBytes != int64(10*1024*1024*1024) {
		t.Fatalf("TotalTrafficBytes = %d, want 10 GiB", status.TotalTrafficBytes)
	}

	wantStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !status.StartDate.Equal(wantStart) {
		t.Fatalf("StartDate = %v, want %v", status.StartDate, wantStart)
	}
	if status.ExpireDate == nil {
		t.Fatal("ExpireDate = nil, want date")
	}
	wantExpire := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	if !status.ExpireDate.Equal(wantExpire) {
		t.Fatalf("ExpireDate = %v, want %v", *status.ExpireDate, wantExpire)
	}

	wantURL := baseURL + "/sub/" + uuid + "/"
	if status.SubscriptionURL != wantURL {
		t.Fatalf("SubscriptionURL = %q, want %q", status.SubscriptionURL, wantURL)
	}
}

func assertPayloadValue(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()

	if got := payload[key]; got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}

func respondJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
