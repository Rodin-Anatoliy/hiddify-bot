# Backlog

## Ждут решения

- Go-практики, минимальный набор (не внедрено, нужно «ок»): `CGO_ENABLED=0` в CI build; gofmt/goimports в `.golangci.yml`; обновить golangci-lint и `go` в go.mod (1.22 уже без поддержки).
- Злоупотребления (раздача подписки, торренты и т. п.): что делаем — см. ответ 2026-09-28, после решения записать в `docs/design/portal.md`.
- Портал и инвайт-коды — вопросы в `docs/design/portal.md`, раздел 9.

## 🔴 Срочное

- Новый пользователь: xhttp работает сразу, основной tcp+reality — только после полного Apply (Hiddify v13, vpn-ops D15). Варианты — `docs/design/portal.md`, раздел 5.

## Дальше

- Портал + инвайт-коды: этапы в `docs/design/portal.md`.
- Мелкие долги из аудита 2026-09-28: игнорируемые ошибки `Edit` в approve/reject (`internal/transport/tg/admin_handlers.go`); нет ретраев к API панели; мало тестов на обработчики Telegram.

## Сделано

- 2026-09-28 — настройка под Claude Code: CLAUDE.md, агенты, скиллы /start и /finish, docs/deploy.md.
