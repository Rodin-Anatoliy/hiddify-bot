# hiddify-bot

Telegram bot for managing [Hiddify Manager](https://github.com/hiddify/HiddifyManager) users.  

## Features

| Command | Who | Description |
|---|---|---|
| `/start` | User | Register; auto-link Hiddify account if `telegram_id` is set in panel |
| `/status` | User | Live subscription stats: traffic, expiry, personal link |
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
  config/         — env loading, validation, defaults
  domain/         — pure entities, no external dependencies
  usecase/        — business logic and interfaces consumed by use cases
  infrastructure/ — implementations: Telegram, Hiddify API, SQLite
pkg/
  apperr/         — sentinel errors
  logger/         — structured JSON logging (slog)
```

Dependency rule: `domain ← usecase ← infrastructure ← cmd`

## TODO
- /bind UI - create a message with input fields in order (tg id, uuid), the window changes and does not produce unnecessary ones
- first start - for new users, add the ability to request a connection (or link) with admin approval. Then, notify the user that the connection has been approved. Create a new user in the panel via the API and return to the user. Create an unlimited user (GB limit - 100000, Package days - 10000, Name - randomly chosen).
- users with several links - study the panel's configuration. Ideally, all links should be subscribed to the same TG, which will limit spam (so that only one message is received), but the goal is for the user to see information on all subscriptions in one account and receive all links.

## License

MIT
