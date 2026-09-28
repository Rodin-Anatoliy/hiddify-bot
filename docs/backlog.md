# Backlog

## Ждут решения

- Go-практики, минимальный набор (не внедрено, нужно «ок»): `CGO_ENABLED=0` в CI build; gofmt/goimports в `.golangci.yml`; обновить golangci-lint и `go` в go.mod (1.22 уже без поддержки).
- `.env.example`: в `HIDDIFY_ADMIN_PROXY` / `HIDDIFY_USER_PROXY` лежат конкретные строки — это настоящие пути панели или выдуманные? Если настоящие — заменить на заглушки и сменить пути.
- `docs/ai/` (CONTEXT, STATUS, INDEX.json) — старая память для ИИ, дублирует CLAUDE.md. Удалить?
- Портал и инвайт-коды — вопросы в `docs/design/portal.md` («Вопросы к Анатолию»).

## 🔴 Срочное

- Новые пользователи не работают до `apply_users` (Hiddify v13) — решение в `docs/design/portal.md`.

## Дальше

- Портал + инвайт-коды: этапы в `docs/design/portal.md`.
- Мелкие долги из аудита 2026-09-28: игнорируемые ошибки `Edit` в approve/reject (`internal/transport/tg/admin_handlers.go`); нет ретраев к API панели; мало тестов на обработчики Telegram.

## Сделано

- 2026-09-28 — настройка под Claude Code: CLAUDE.md, агенты, скиллы /start и /finish, docs/deploy.md.
