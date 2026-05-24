# hiddify-bot

Telegram bot for managing [Hiddify Manager](https://github.com/hiddify/HiddifyManager) users.  

## Features

| Command | Who | Description |
|---|---|---|
| `/start` | User | Register; auto-link Hiddify account if `telegram_id` is set in panel |
| `/status` | User | Live stats and links for all profiles tied to this Telegram ID |
| `/support` | User | Two-way support channel with admin |
| `/bind <tg_id> <uuid>` | Admin | Manually link a Telegram user to a Hiddify account |
| `/sync` | Admin | Pull all panel users with `telegram_id` into local DB |
| `/broadcast <text>` | Admin | Send text or photo to all active users |
| `/users` | Admin | List Hiddify users with Telegram/bot status |
| `/users unbound` | Admin | List Hiddify users without `telegram_id` |
| `/users blocked` | Admin | List linked users the bot cannot message |
| `/history <tg_id>` | Admin | View support message history for a user |

## Deployment

Runs as a **systemd service** on the same server as Hiddify — no Docker needed.  
GitHub Actions builds a static binary and deploys it automatically on every push to `main`.
Optimized for minimal setup VPS.

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
  config/          — env loading, validation, defaults
  domain/          — pure entities, ports, sentinel business errors
    admin/         — admin reply/bind session model and port
    subscription/  — subscription status and Hiddify-facing port
    ticket/        — support message model and port
    user/          — bot user model and port
  service/         — business use cases and orchestration
  repository/      — adapter implementations
    hiddify/       — Hiddify Manager API adapter
    sqlite/        — SQLite persistence adapter
  transport/
    tg/            — Telegram bot, handlers, delivery
      markup/      — inline keyboard builders
      views/       — Telegram-facing text rendering
pkg/
  logger/          — structured JSON logging (slog)
```

Dependency rule: `transport/tg -> service -> domain`; repositories implement domain ports and are wired in `cmd/bot`.

AI-oriented project notes live in `docs/ai/`.

## TODO
- Decide whether multi-subscription support needs local per-profile persistence after `/status` usage is validated.
- Improve admin UX by turning command-heavy flows into editable Telegram panels/wizards.

## License

MIT
