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

## Quick start

```bash
git clone https://github.com/Rodin-Anatoliy/hiddify-bot
cd hiddify-bot

cp .env.example .env
# Fill in TG_TOKEN, TG_ADMIN_ID, HIDDIFY_* values

mkdir -p data
make deploy        # builds binary and starts the bot
```

For local development:

```bash
go mod download
make dev
```

See [.github/DEVELOPMENT.md](.github/DEVELOPMENT.md) for full setup guide.

## Architecture

```
internal/
  domain/         — pure entities, no external dependencies
  port/           — outbound interface contracts (Sender)
  usecase/        — business logic, depends only on domain + port
  infrastructure/ — implementations: Telegram, Hiddify API, SQLite
  config/         — configuration loading
pkg/
  apperr/         — sentinel errors
  logger/         — structured JSON logging (slog)
```

Dependency rule: `domain ← port ← usecase ← infrastructure ← cmd`  
`usecase` never imports `infrastructure`.

## Configuration

All settings live in `.env`:

```env
TG_TOKEN=...
TG_ADMIN_ID=...
HIDDIFY_BASE_URL=https://your-panel.example.com
HIDDIFY_ADMIN_PROXY=your-random-path
HIDDIFY_API_KEY=your-api-key
```

## User linking flow

Telegram does not allow bots to initiate conversations. The flow:

1. Create user in Hiddify panel → set their `telegram_id`
2. Run `/sync` → user appears in local DB (`can_message = false`)
3. User opens the bot and presses `/start` → `can_message = true`
4. Broadcast reaches them

## Deployment

The bot is deployed via GitHub Actions CI/CD. On push to `main`, it runs tests, builds a Linux binary, and deploys it to the production server.

### Setup CI/CD
1. In your GitHub repo, go to Settings > Secrets and variables > Actions.
2. Add secrets:
   - `SERVER_HOST`: Your server IP/hostname (e.g., `vm703859.yourhost.com`)
   - `SERVER_USER`: SSH username (e.g., `pro100click`)
   - `SERVER_SSH_KEY`: Private SSH key for access (generate with `ssh-keygen`, add public key to server's `~/.ssh/authorized_keys`)

### Production Setup
On the server (`/opt/hiddify-bot/`):
- `.env` with your secrets
- `data/` directory for SQLite DB
- systemd service: `/etc/systemd/system/hiddify-bot.service`

Example service file:
```
[Unit]
Description=Hiddify Bot
After=network.target

[Service]
Type=simple
User=pro100click
WorkingDirectory=/opt/hiddify-bot
ExecStart=/opt/hiddify-bot/hiddify-bot
EnvironmentFile=/opt/hiddify-bot/.env
Restart=always

[Install]
WantedBy=multi-user.target
```

Enable and start: `sudo systemctl enable hiddify-bot && sudo systemctl start hiddify-bot`

### Manual Deploy (if needed)
```bash
make build-linux  # on your machine
scp bin/hiddify-bot user@server:/opt/hiddify-bot/
# Then on server: sudo systemctl restart hiddify-bot
```
