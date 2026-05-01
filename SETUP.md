# Запуск Hiddify Bot

## Требования

- Go 1.22+
- Docker, если нужен контейнерный запуск
- Hiddify Manager
- Telegram bot token от BotFather

## Настройка

`config.yml` — основной файл настроек. Он должен быть рядом с приложением и в Docker монтируется в контейнер.

В `config.yml` не хранятся реальные секреты. Секретные поля заполняются через `.env`:

```bash
cp .env.example .env
```

Заполните `.env`:

```env
TG_TOKEN=1234567890:token
TG_ADMIN_ID=123456789
HIDDIFY_BASE_URL=https://panel.example.com
HIDDIFY_ADMIN_PROXY=random-admin-path
HIDDIFY_API_KEY=api-key
DB_PATH=data/bot.db
LOG_LEVEL=info
TG_TIMEOUT=10
```

`TG_ADMIN_ID` можно узнать через Telegram-бота `@userinfobot`.

Если `config.yml` отсутствует:

```bash
cp config.yml.example config.yml
```

Пример из `config.yml`:

```yaml
telegram:
  token: "${TG_TOKEN}"
  admin_id: ${TG_ADMIN_ID}
  timeout: ${TG_TIMEOUT:-10}
```

Так видно, какие настройки нужны приложению, но секреты остаются в `.env`.

## Dev: локальный запуск

```bash
go mod tidy
make dev
```

## Prod: Docker из исходников

Основной рабочий вариант. На сервере лежит репозиторий проекта, compose собирает образ локально из `Dockerfile`. Registry и push не нужны.

```bash
docker compose up -d
docker compose logs -f
docker compose down
```

На сервере нужны:

- `docker-compose.yml`
- `Dockerfile`
- исходники проекта
- `config.yml`
- `.env`
- папка `data`

## Проверка

1. Откройте бота в Telegram.
2. Нажмите `/start`.
3. Если в Hiddify уже указан ваш `telegram_id`, привязка появится автоматически.
4. Проверьте `/status`.

## Команды администратора

| Команда | Назначение |
|---|---|
| `/bind <telegram_id> <uuid>` | Вручную привязать Telegram к пользователю Hiddify |
| `/sync` | Подтянуть привязки из Hiddify по заполненному `telegram_id` |
| `/broadcast <text>` | Отправить текст всем доступным привязанным пользователям |

## Логика привязки

`/bind` и `/sync` могут создать локальную привязку заранее, но рассылки будут работать только после того, как пользователь сам откроет бота и нажмет `/start`.

Это ограничение Telegram, а не приложения.

## Тесты

```bash
make test
```

`make test-race` требует `CGO_ENABLED=1` и C-компилятор.
