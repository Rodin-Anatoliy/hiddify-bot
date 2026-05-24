# Project Status & Task Board

## Current Stage: Architecture Stabilized
**Goal:** Keep the completed Go layered architecture stable while tightening docs, tests, and small domain seams.

## Technical Debt & Known Issues (WATCH OUT)
- **Naming:** `TelegramID` and `HiddifyUUID` are explicit domain fields. Acceptable for this bot, but creates coupling if other transports/providers are added.
- **Service Leakage:** `service.PanelUserView` is used in transport. Keep it as a pure data DTO.
- **Hardcoded Defaults:** `repository/hiddify/create_user.go` has fixed usage/package defaults. Move to config later if policy becomes variable.
- **Testing:** Repository and transport layers lack focused unit tests.

## Active Roadmap
1. [x] **Architecture:** Move to `domain/service/repository/transport`.
2. [x] **Docs:** Align README and `docs/ai` with current structure.
3. [x] **Tests:** Expand tests for `repository/sqlite` with a temporary DB.
4. [x] **Tests:** Add Hiddify adapter mapping tests with `httptest`.
5. [x] **Hardening:** Review broadcast cancellation/rate-limit behavior.
6. [ ] **Config:** Move Hiddify create-user defaults to config if they become deployment policy.

## Maintenance Rules
- After structural changes: Update `INDEX.json` and this `STATUS.md`.
- Run validation: `go test ./...` and `go list ./...`.
