# Деплой (как есть, 2026-09-28)

Описание текущего состояния. Менять — отдельной задачей с «ок».

## CI/CD — `.github/workflows/deploy.yml`

Триггер: push в `main`. Задачи по цепочке:

1. **lint** — golangci-lint v1.62.2 (`.golangci.yml`: errcheck, govet, ineffassign, staticcheck, unused), timeout 5m.
2. **test** — `go test -race -cover ./...`.
3. **build** — `GOOS=linux GOARCH=amd64 go build -o bin/hiddify-bot ./cmd/bot`, артефакт `hiddify-bot-binary`.
   `CGO_ENABLED=0` в CI не задан (в `make build-linux` — задан). SQLite-драйвер чистый Go, так что бинарник, скорее всего, всё равно работает, но полностью статическим он гарантированно будет только с `CGO_ENABLED=0`.
4. **deploy** — `appleboy/ssh-action`: `systemctl stop hiddify-bot`, создать `/opt/hiddify-bot`; `appleboy/scp-action`: бинарник в `/opt/hiddify-bot/`; ssh: `chmod +x`, `systemctl restart hiddify-bot`, проверка статуса.

Секреты GitHub (имена): `SERVER_HOST`, `SERVER_USER`, `SERVER_SSH_KEY`, `SERVER_SSH_PASSPHRASE`.

## Сервер de-1

- Unit `deploy/hiddify-bot.service`: `User=root`, `WorkingDirectory=/opt/hiddify-bot`, `EnvironmentFile=/opt/hiddify-bot/.env`, `ExecStart=/opt/hiddify-bot/hiddify-bot`, `Restart=on-failure` (5s).
- `/opt/hiddify-bot/`: бинарник, `.env` (секреты), `data/bot.db` (SQLite).
- Первичная настройка — `deploy/setup.sh`: каталог `data/`, копия unit, `daemon-reload`, `enable`.
- Бэкап `data/` и `.env` делается в проекте vpn-ops (`backups/de-1/<дата>/`).

## Откат

Отдельного механизма нет: откатить = `git revert` + push (новый деплой). Перед рискованным деплоем — снимок `data/bot.db` на сервере (только с «ок»).

## Цель `make deploy`

Устаревшая: собирает и запускает бинарник локально через `nohup` — к деплою на сервер отношения не имеет.
