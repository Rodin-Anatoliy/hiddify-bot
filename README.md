# hiddify-bot

Telegram bot for managing [Hiddify Manager](https://github.com/hiddify/HiddifyManager) users.  
Built as a Go portfolio project — Clean Architecture, testable, production-ready.

## Features

| Command | Who | Description |
|---|---|---|
| `/start` | User | Register; auto-link Hiddify account if `telegram_id` is set in panel |
| `/status` | User | Live subscription stats: traffic, expiry, personal link |
| `/support` | User | Two-way support channel with admin |
| `/bind <tg_id> <uuid>` | Admin | Manually link a Telegram user to a Hiddify account |
| `/sync` | Admin | Pull all panel users with `telegram_id` into local DB |
| `/broadcast <text>` | Admin | Send text or photo to all active users |
| `/users` | Admin | List all linked users with messaging status |
| `/history <tg_id>` | Admin | View support message history for a user |

## Deployment

Runs as a **systemd service** on the same server as Hiddify — no Docker needed.  
GitHub Actions builds a static binary and deploys it automatically on every push to `main`.

### First-time server setup

```bash
# On the server (once)
cd /tmp
git clone https://github.com/Rodin-Anatoliy/hiddify-bot
cd hiddify-bot/deploy
bash setup.sh

# Copy your .env to the server
scp .env user@server:/opt/hiddify-bot/.env
```

### GitHub Secrets required

Go to **Settings → Secrets → Actions** in your repo and add:

| Secret | Value |
|---|---|
| `SSH_HOST` | Server IP or hostname |
| `SSH_USER` | SSH username (e.g. `root`) |
| `SSH_PRIVATE_KEY` | Contents of your private key (`~/.ssh/id_rsa`) |
| `SSH_PORT` | SSH port (usually `22`) |

After that — every push to `main` triggers lint → test → deploy automatically.

## Local development

```bash
cp .env.example .env   # fill in your credentials
go mod download
mkdir -p data
make dev               # go run, hot on save
```

## Architecture

```
internal/
  config/         — env loading, validation, defaults
  domain/         — pure entities, no external dependencies
  port/           — outbound interface contracts (Sender)
  usecase/        — business logic, depends only on domain + port
  infrastructure/ — implementations: Telegram, Hiddify API, SQLite
pkg/
  apperr/         — sentinel errors
  logger/         — structured JSON logging (slog)
```

Dependency rule: `domain ← port ← usecase ← infrastructure ← cmd`

## License

MIT
