# Agent rules — hiddify-bot

## Project
Go 1.22 (1.26 locally), a Telegram bot (telebot.v3, long polling) for users of Hiddify Manager v13 on server de-1.
- It calls the panel API and stores data in SQLite (`modernc.org/sqlite`, no cgo).
- Deploy: push to `main` → GitHub Actions (lint → test → build → scp → systemd on de-1).
- **The repo is public.**

## Never (violation = «Критично»)
- Push: it deploys to prod.
- Read or edit `.env`, `data/`, `go.sum`.
- Commit any secret: bot token, panel API key, user UUIDs, secret admin path, subscription links, server IP or domain, passwords. `.env`, `data/*.db` and binaries must never be in the index.
- Run the bot locally with the prod token. A second long poll returns 409 to both clients and breaks prod.
- Touch server de-1 unless the brief allows it, and then read-only. Hiddify settings and its haproxy/nginx/xray are off-limits entirely.
- Write to the panel API (create/modify/delete users, apply) from the local environment. Only in code, and in tests against `httptest`.
- Change the SQLite schema without a migration in the same commit as the code.
- Edit `.github/workflows/`, `deploy/`, `.golangci.yml`, or `go.mod` (new deps), unless the brief says so.
- Swallow errors (`_ =` on significant calls, empty `if err != nil {}`).

## Go conventions
- Wrap errors with context: `fmt.Errorf("what we did: %w", err)`.
- Logging: `log/slog` via DI (`pkg/logger`).
- Config comes only from env (`internal/config`). Add new variables to `.env.example` with a comment.
- Layers: domain / repository / service / transport under `internal/`; the entry point is `cmd/bot`.
- Panel HTTP in tests goes through `httptest.Server`.
- Reviewer focus:
  - races: goroutines, shared maps, context cancellation;
  - leaks: `rows`, `Body`;
  - HTTP timeouts;
  - user input, SQL parameters only, attempt limits, admin rights.

## Checks
- Run `gofmt -l .` (must be empty), `go vet ./...`, `go build ./...`, `go test -count=1 ./...`.
- `-race` doesn't work locally (no cgo); write «race — только в CI».
- golangci-lint runs in CI only. If needed: `go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2 run`; if it won't build, write «не проверено».

## Map
- `cmd/` — entry point
- `internal/` — logic
- `pkg/` — shared
- `deploy/` — systemd and setup
- `.github/workflows/` — CI and deploy
- `docs/` — documentation
