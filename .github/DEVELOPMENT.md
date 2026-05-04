# Development Guide

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.24+ | https://go.dev/dl/ |
| golangci-lint | latest | https://golangci-lint.run/usage/install/ |
| Docker | 24+ | https://docs.docker.com/get-docker/ |

## Local setup

```bash
git clone https://github.com/Rodin-Anatoliy/hiddify-bot
cd hiddify-bot

cp .env.example .env   # fill in your credentials
go mod download
mkdir -p data
make dev
```

## Configuration

All settings live in a single `.env` file. No `config.yml` needed.

| Variable | Required | Default | Description |
|---|---|---|---|
| `TG_TOKEN` | ✅ | — | Bot token from @BotFather |
| `TG_ADMIN_ID` | ✅ | — | Your Telegram ID (@userinfobot) |
| `HIDDIFY_BASE_URL` | ✅ | — | Panel URL, no trailing slash |
| `HIDDIFY_ADMIN_PROXY` | ✅ | — | Random path from panel URL |
| `HIDDIFY_API_KEY` | ✅ | — | Admin API key from panel Settings |
| `DB_PATH` | — | `data/bot.db` | SQLite file location |
| `LOG_LEVEL` | — | `info` | debug/info/warn/error |
| `TG_TIMEOUT` | — | `10` | Long-polling timeout (seconds) |

## Commands

```bash
make dev          # run locally via go run
make test         # tests with race detector
make cover        # test + HTML coverage report
make lint         # golangci-lint
make fmt          # gofumpt
make tidy         # go mod tidy + verify
make deploy       # docker compose up -d --build
make docker-logs  # tail container logs
make docker-down  # stop
```

## Architecture

```
cmd/bot/            ← main(): wires everything, zero business logic
internal/
  config/           ← env loading, validation, defaults
  domain/           ← pure entities + repository interfaces
  port/             ← outbound interface contracts (Sender)
  usecase/          ← business logic; imports only domain + port
  infrastructure/   ← Telegram, Hiddify API, SQLite
pkg/
  apperr/           ← sentinel errors
  logger/           ← slog JSON wrapper
```

Dependency rule: `domain ← port ← usecase ← infrastructure ← cmd`

## Before a PR

```bash
make tidy && make fmt && make lint && make test
```
