package hiddify_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain"
	"github.com/Rodin-Anatoliy/hiddify-bot/internal/domain/subscription"
)

func TestClientCreateUserTariffOverridesWinOverDefaults(t *testing.T) {
	t.Parallel()

	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodPost, "/admin/api/v2/admin/user/")

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		assertPayloadValue(t, payload, "name", "Bob")
		assertPayloadValue(t, payload, "telegram_id", float64(43))
		assertPayloadValue(t, payload, "usage_limit_GB", float64(200))
		assertPayloadValue(t, payload, "package_days", float64(30))
		assertPayloadValue(t, payload, "mode", "monthly")
		// Not overridable: still from the defaults.
		assertPayloadValue(t, payload, "enable", true)
		assertPayloadValue(t, payload, "lang", "ru")
		if _, ok := payload["start_date"]; ok {
			t.Fatal("start_date must not be sent (the panel starts the countdown at first use)")
		}

		respondJSON(t, w, map[string]any{"uuid": "uuid-2"})
	}))
	defer server.Close()

	created, err := newTestClient(server.URL).CreateUser(context.Background(), subscription.CreateUserRequest{
		Name:         "Bob",
		TelegramID:   43,
		UsageLimitGB: 200,
		PackageDays:  30,
		Mode:         "monthly",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if created.UUID != "uuid-2" {
		t.Fatalf("UUID = %q", created.UUID)
	}
	// ExpiresAt follows the days actually sent, not the default 10000.
	if created.ExpiresAt.Before(start.AddDate(0, 0, 29)) || created.ExpiresAt.After(start.AddDate(0, 0, 31)) {
		t.Fatalf("ExpiresAt = %v, want about 30 days from now", created.ExpiresAt)
	}
}

func TestClientCreateUserPartialOverrideKeepsOtherDefaults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		assertPayloadValue(t, payload, "usage_limit_GB", float64(100000))
		assertPayloadValue(t, payload, "package_days", float64(7))
		assertPayloadValue(t, payload, "mode", "no_reset")
		respondJSON(t, w, map[string]any{"uuid": "uuid-3"})
	}))
	defer server.Close()

	if _, err := newTestClient(server.URL).CreateUser(context.Background(), subscription.CreateUserRequest{
		Name: "Carol", TelegramID: 44, PackageDays: 7,
	}); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
}

func TestClientCreateUserErrorKeepsStatusForClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status    int
		wantClear bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusForbidden, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusTooManyRequests, false},
		{http.StatusRequestTimeout, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusNotFound, false},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", tt.status)
			}))
			defer server.Close()

			_, err := newTestClient(server.URL).CreateUser(context.Background(), subscription.CreateUserRequest{Name: "x", TelegramID: 1})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := domain.IsClearRejection(err); got != tt.wantClear {
				t.Fatalf("IsClearRejection(%d) = %v, want %v (err = %v)", tt.status, got, tt.wantClear, err)
			}
			if tt.status != http.StatusNotFound && !errors.Is(err, domain.ErrHiddifyAPI) {
				t.Fatalf("err = %v, want it to wrap ErrHiddifyAPI", err)
			}
		})
	}
}

func TestClientCreateUserEmptyUUIDIsNotSuccessAndNotARejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(t, w, map[string]any{"name": "x"})
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).CreateUser(context.Background(), subscription.CreateUserRequest{Name: "x", TelegramID: 1})
	if err == nil {
		t.Fatal("empty uuid must be an error")
	}
	if domain.IsClearRejection(err) {
		t.Fatal("empty uuid must not be treated as a clear rejection")
	}
}

func TestClientCreateUserTimeoutIsNotARejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // connection refused

	_, err := newTestClient(server.URL).CreateUser(context.Background(), subscription.CreateUserRequest{Name: "x", TelegramID: 1})
	if err == nil {
		t.Fatal("expected a network error")
	}
	if domain.IsClearRejection(err) {
		t.Fatal("network error must not be treated as a clear rejection")
	}
}
