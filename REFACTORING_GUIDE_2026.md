# 🏗 ARCHITECTURAL_GUIDE_2026.md

## 1. STRATEGIC GOAL
**Architecture Pattern:** Pragmatic Layered DDD (Domain-Driven Design) / Modular Monolith.
**Core Principle:** Strict Separation of Concerns (SoC). Business logic must be agnostic of external APIs (Hiddify) and Delivery Mechanisms (Telegram).

---

## 2. REFINED DIRECTORY STRUCTURE (TARGET)

| Layer | Directory | Responsibility |
| :--- | :--- | :--- |
| **ENTRY** | `cmd/bot/` | Main entry point. Dependency Injection (DI) container initialization. |
| **DOMAIN** | `internal/domain/` | Pure business entities (User, Subscription). Interfaces for repositories. **No dependencies.** |
| **SERVICE** | `internal/service/` | Business Logic / Use Cases. Orchestrates data flow between repositories. |
| **REPO** | `internal/repository/` | Data Access Layer. Adapters for SQLite (DB) and Hiddify (External API). |
| **TRANS** | `internal/transport/tg/` | Telegram-specific layer. Handlers, Middlewares, and UI (Markups/Views). |

---

## 3. MAPPING (OLD TO NEW)

- `internal/delivery/telegram` -> `internal/transport/tg/handlers`
- `internal/infrastructure/hiddify` -> `internal/repository/hiddify` (Treat as a data source)
- `internal/infrastructure/repository` -> `internal/repository/sqlite`
- `internal/infrastructure/telegram` -> `internal/transport/tg`
- `internal/usecase` -> `internal/service`
- `internal/errs` -> Merge into `internal/domain` or use standard errors.

---

## 4. REFACTORING PROTOCOLS FOR LLM

### Protocol A: Dependency Flow
- **Allowed:** `Transport -> Service -> Repository -> Domain`
- **Forbidden:** Any circular dependencies. `Service` must NOT know about `tgbotapi`.

### Protocol B: Logic Extraction
1. **Repository:** Must only return `domain` entities. If Hiddify API returns a complex JSON, map it to a simple `domain.User` struct immediately.
2. **Service:** Must be "pure". No Telegram buttons or formatting here. Just logic: `CheckAccess()`, `ActivateSub()`.
3. **Transport:** - `handlers/`: Handle Telegram updates, extract params, call Service.
    - `markup/`: (Old `keyboards.go`) Only generate UI elements.
    - `views/`: (Old `format.go`) Only generate string templates.

### Protocol C: Error Handling
- Use custom errors defined in `internal/domain` (e.g., `ErrSubscriptionExpired`).
- Transport layer maps these errors to user-friendly Telegram messages.

---

## 5. PROMPT FOR AI REFACTORING (COPY-PASTE)

> "Role: Senior Go Architect.
> Task: Refactor the provided Go code following 'ARCHITECTURAL_GUIDE_2026.md'.
> Constraints:
> 1. Decouple logic from 'tgbotapi'. Move all UI (buttons/text) to 'transport/tg/markup' and 'views'.
> 2. Move Hiddify API calls to 'repository/hiddify'. It must implement an interface from 'domain'.
> 3. Use 'internal/service' for all business decisions.
> 4. Ensure zero circular imports. 
> 5. Output idiomatic Go code (Uber style guide compliant)."

---

## 6. DEFINITION OF DONE
- [ ] No Telegram-specific code in `internal/service`.
- [ ] Hiddify API is treated as a Repository, not "Infrastructure".
- [ ] Code is unit-testable (Service can be tested with Mock Repositories).
- [ ] Folder `internal/delivery` and `internal/infrastructure` are DELETED.
