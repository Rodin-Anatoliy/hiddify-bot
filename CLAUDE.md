# hiddify-bot

Telegram bot for the users of Anatoliy's VPN (Hiddify Manager v13 on server de-1): issuing subscriptions and showing their status, support, broadcasts, admin commands. Global rules: `~/.claude/CLAUDE.md`. Agent rules: `.claude/agent-rules.md`.

## Critical
1. **Push only after «ок»:** a push to `main` deploys to prod (Actions: lint → test `-race` → build → scp → `systemctl restart hiddify-bot`).
2. **The repo is public.** Never commit or show the bot token, the panel API key, user UUIDs, the secret panel path, subscription links, or the server IP or domain. They live only in `.env`, both locally and in `/opt/hiddify-bot/.env` on the server. `.env` is also denied in settings.
3. **Server de-1 is read-only.** Change it only after «ок». Its address and access are in `../vpn/servers/de-1.md`; never copy them here. Anatoliy's PC reaches the internet through the VPN on de-1.
4. **Don't touch Hiddify settings or its haproxy/nginx/xray.** They belong to vpn-ops (`../vpn`), which keeps the server decision log.
5. **Never run the bot locally with the prod token.** A second long-polling client on the same token breaks prod (Telegram returns 409 to both). For local runs, use a separate test token.
6. **Panel API writes** (create/modify users, apply) happen only from code running on the server. Locally, use `httptest` only.
7. **SQLite schema changes** only by migration, in the same commit as the code that uses it.

Risk in this project:
- low: docs only;
- medium: bot code, which goes to prod on push;
- high: the server, panel API writes, the bot DB schema.

## Stack and layout
Go 1.22 (1.26 locally), telebot.v3 (long polling), `modernc.org/sqlite` (no cgo), `log/slog` JSON.

| Path | What |
|---|---|
| `cmd/bot/` | entry point, wiring |
| `internal/config/` | env config (`MustLoad`, validation); sample in `.env.example` |
| `internal/domain/` | user, subscription, ticket, admin |
| `internal/repository/hiddify/` | panel API client `/<admin_proxy>/api/v2/admin/user/`, header `Hiddify-API-Key` |
| `internal/repository/sqlite/` | bot DB (users, ticket_messages, admin_sessions), migrations |
| `internal/service/` | users, support, broadcasts |
| `internal/transport/tg/` | Telegram commands and buttons |
| `pkg/logger/` | slog wrapper |
| `deploy/` | systemd unit, `setup.sh`; details in `docs/deploy.md` |
| `docs/decisions.md`, `docs/backlog.md`, `docs/design/` | decisions, backlog, feature designs |

## Commands (Windows: no make, gcc, golangci-lint)
- `gofmt -l .` must be empty · `go vet ./...` · `go build ./...` · `go test -count=1 ./...`
- `-race` and golangci-lint run in CI only.

## Known
- Hiddify v13: a user created through the API gets xhttp immediately, but the main tcp+reality works only after a full Apply (vpn-ops D15). The bot doesn't and must not call Apply. Options are in `docs/design/portal.md` §5.
