# Project Status & Task Board

## Current Stage: Architecture Stabilized
**Goal:** Add user-visible multi-profile support while keeping the completed layered architecture stable.

## Technical Debt & Known Issues (WATCH OUT)
- **Naming:** `TelegramID` and `HiddifyUUID` are explicit domain fields. Acceptable for this bot, but creates coupling if other transports/providers are added.
- **Service Leakage:** `service.PanelUserView` is used in transport. Keep it as a pure data DTO.
- **Config Defaults:** Hiddify create-user policy is loaded from env and passed through the composition root.
- **Multi-profile Direction:** Hiddify can hold multiple profiles with the same Telegram ID. User status should show all profiles, but broadcast must remain one message per Telegram user.
- **Testing:** Repository and transport layers lack focused unit tests.

## Active Roadmap
1. [x] **Architecture:** Move to `domain/service/repository/transport`.
2. [x] **Docs:** Align README and `docs/ai` with current structure.
3. [x] **Tests:** Expand tests for `repository/sqlite` with a temporary DB.
4. [x] **Tests:** Add Hiddify adapter mapping tests with `httptest`.
5. [x] **Hardening:** Review broadcast cancellation/rate-limit behavior.
6. [x] **Config:** Move Hiddify create-user defaults to config if they become deployment policy.
7. [x] **Feature:** Show all Hiddify profiles linked to a Telegram user in `/status`.
8. [ ] **Model:** Decide whether local DB needs a separate profile table after multi-profile usage is proven.
9. [ ] **Admin UX:** Convert command-heavy admin flows to editable Telegram panels/wizards.

## Maintenance Rules
- After structural changes: Update `INDEX.json` and this `STATUS.md`.
- Run validation: `go test ./...` and `go list ./...`.
