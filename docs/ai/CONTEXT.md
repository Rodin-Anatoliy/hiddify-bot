# Project AI Context & Architectural Guide

## Purpose
**hiddify-bot** is a Go-based (1.22) Telegram bot for managing Hiddify Manager users. 
- **Core Logic:** Registration, subscription status checks, access requests, support messaging, and admin broadcasts.
- **Architecture:** Pragmatic layered DDD / Modular Monolith.

## Tech Stack
- **Framework:** `gopkg.in/telebot.v3` (Transport only)
- **Database:** `modernc.org/sqlite` (Repository only)
- **External API:** Hiddify Manager API v2 (Repository only)
- **Logging:** `log/slog` (Structured JSON)
- **Concurrency:** `golang.org/x/sync/errgroup`

## Architectural Constraints (CRITICAL)
Follow these rules strictly to maintain layer isolation:

1. **Domain Purity:** `internal/domain` must depend ONLY on the standard library. No transport, service, or repository imports.
2. **Transport Isolation:** `gopkg.in/telebot.v3` (and any UI/Markdown logic) is FORBIDDEN outside `internal/transport/tg`.
3. **Repository Isolation:** - SQL queries and SQLite drivers are FORBIDDEN outside `internal/repository/sqlite`.
    - HTTP clients and Hiddify API schemas are FORBIDDEN outside `internal/repository/hiddify`.
4. **Service Boundary:** Services coordinate domain logic and ports. They must NOT know about Telegram, SQL, or HTTP details.
5. **Dependency Flow:** `Transport -> Service -> Domain Ports -> Repository Implementations`.
6. **No Circular Imports:** `go list ./...` must always pass.

## Error Handling Strategy
- Sentinel business errors (e.g., `ErrNotFound`, `ErrHiddifyAPI`) live in `internal/domain`.
- Repositories must map adapter-specific errors (SQL/HTTP) to domain errors.
- Services wrap errors with operational context.
- Transport maps domain errors to user-facing Telegram messages.

## Key Business Flows
- **Registration:** Handled in `internal/service/user.go`.
- **Sync/Approval:** Orchestration of Hiddify panel and local DB lives in `service`.
- **Support:** Messages persisted via `service/support.go`, delivered via `transport/tg`.
- **Broadcast:** Recipient selection in `service`, delivery and rate-limiting in `transport/tg`.

## Definitions & Identifiers
- **TelegramID:** User identifier from Telegram (used in transport and domain).
- **HiddifyUUID:** Identifier for the Hiddify panel user (used in repository and domain).
- **Note:** Domain identifiers are currently pragmatic and platform-coupled.

---
*Refer to `INDEX.json` for the full directory tree and package mapping.*
