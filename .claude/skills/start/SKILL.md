---
name: start
description: Начало сессии hiddify-bot — состояние репо, backlog, решения, лимиты; выбор задачи и план до «ок».
disable-model-invocation: true
allowed-tools: Bash(git status:*), Bash(git log:*), Bash(git grep:*), Bash(cat:*), Bash(echo:*), PowerShell(git status:*), PowerShell(git log:*), PowerShell(git grep:*), PowerShell(cat:*), PowerShell(echo:*)
---

# Состояние

## git status --short

!`git status --short 2>&1 || echo "(git status не выполнился)"`

## git log --oneline -15

!`git log --oneline -15 2>&1 || echo "(git log не выполнился)"`

## Не запушено

!`git log --oneline "@{u}..HEAD" 2>&1 || echo "(нет upstream — проверить git remote)"`

## Лимиты на старте (~/.claude/usage.txt)

!`cat ~/.claude/usage.txt 2>&1 || echo "(usage.txt нет — statusline ещё не записал лимиты; расход задачи будет не посчитать)"`

## Верх docs/backlog.md

!`git grep --no-index -h -m 30 "" -- docs/backlog.md 2>&1 || echo "(docs/backlog.md не найден — сказать Анатолию)"`

## Решения (docs/decisions.md, только заголовки)

!`git grep --no-index -h "^## D" -- docs/decisions.md 2>&1 || echo "(записей D<N> пока нет)"`

# Что сделать

1. Сверить записанное с реальностью: незакоммиченное, незапушенное, свежие коммиты, backlog. Противоречие — сначала сказать Анатолию.
2. Спросить, чем занимаемся. Не назвал — предложить 1–3 задачи из backlog (сначала «Ждут решения» и 🔴).
3. Задача трогает область из заголовков решений — открыть эту запись в `docs/decisions.md` и проверить «Пересмотреть, если». Условие наступило — сказать до плана.
4. Код, логи, вывод с сервера самой не читать — разведка `explorer`, код `executor`, проверка `reviewer`. Справка — `CLAUDE.md`, `docs/deploy.md`, `docs/design/*` нужным разделом.
5. Дать план:
   - задача и этапы; каждый этап кода — новый `executor`, после него `reviewer`;
   - риск: нет (только чтение/документы) / средний (код бота, уйдёт в прод при пуше) / высокий (сервер, API панели на запись, схема БД бота);
   - ожидаемый расход в % недели по аналогии с прошлыми; аналогий нет — так и сказать. Лимит 5ч или 7д близко — сказать и предложить границу этапа, где остановиться.
6. Ждать «ок». До него ничего не запускать и не менять.

Значения лимитов выше — точка отсчёта для `/finish`, не терять их.
