# Project Structure

## Overview

```
hiddify-bot/
├── .github/
│   ├── workflows/
│   │   └── ci.yml              # GitHub Actions: lint → test → build
│   └── DEVELOPMENT.md          # Development guide
├── cmd/
│   └── bot/
│       └── main.go             # Application entry point
├── internal/                   # Private code (enforced by Go)
│   ├── domain/
│   │   ├── subscription/
│   │   ├── ticket/
│   │   └── user/               # Business entities
│   ├── infrastructure/         # External integrations
│   │   ├── config/
│   │   ├── hiddify/            # Hiddify API client
│   │   ├── repository/         # SQLite database
│   │   └── telegram/           # Telegram bot
│   └── usecase/                # Application logic
│       ├── broadcast.go        # Mass messaging
│       ├── support.go          # User support tickets
│       └── user.go             # User management
├── pkg/                        # Public packages
│   ├── apperr/                 # Custom errors
│   └── logger/                 # Logging
├── data/                       # Runtime data
│   └── *.db                    # SQLite database (runtime)
├── Makefile                    # Build commands
├── Dockerfile                  # Multi-stage Docker build
├── docker-compose.yml          # Local Docker Compose
├── .golangci.yml               # Lint configuration
├── go.mod                      # Go dependencies
├── go.sum                       # Dependency checksums
└── README.md                   # Project documentation
```

## Architecture

### Clean Architecture Layers

```
┌─────────────────────────────────┐
│   cmd/bot/main.go (Entry)       │ ← Application
├─────────────────────────────────┤
│   internal/infrastructure/      │ ← Frameworks & Drivers
│   - telegram (framework)        │
│   - hiddify (HTTP client)       │
│   - repository (database)       │
├─────────────────────────────────┤
│   internal/usecase/             │ ← Application Business Rules
│   - broadcast, support, user    │
├─────────────────────────────────┤
│   internal/domain/              │ ← Enterprise Business Rules
│   - user, subscription, ticket  │
├─────────────────────────────────┤
│   pkg/                          │ ← Utilities
│   - apperr, logger              │
└─────────────────────────────────┘
```

### Component Responsibilities

- **cmd/bot**: Entry point, dependency injection, graceful shutdown
- **internal/domain**: Pure business logic entities (no dependencies)
- **internal/usecase**: Use cases, orchestrates domain + infrastructure
- **internal/infrastructure**: External services integration (DB, APIs, messaging)
- **pkg**: Shared utilities (logging, error handling)

## Build & Deployment

### Development Workflow
```bash
go run ./cmd/bot       # Local development
go test ./...          # Run tests
gofmt -w .             # Format code
golangci-lint run      # Lint
```

### Docker Deployment
```bash
docker build -t hiddify-bot:latest .
docker run hiddify-bot:latest
```

### CI/CD Pipeline

1. **Trigger**: Push to main/develop or PR
2. **Lint**: Code formatting and static analysis
3. **Test**: Unit tests with coverage
4. **Build**: Docker image verification
5. **Result**: Pass/Fail status on PR

## Naming Conventions

| Type | Example | Rule |
|------|---------|------|
| Packages | `user`, `telegram` | lowercase, single word |
| Types | `User`, `Bot` | PascalCase |
| Functions | `GetUser()`, `SendMessage()` | camelCase |
| Constants | `MAX_RETRIES` | UPPER_CASE |
| Files | `user.go`, `bot_test.go` | lowercase_snake |
| Database Tables | `users`, `user_roles` | plural_lowercase |

## Dependencies

### Core
- Go 1.22+
- gopkg.in/telebot.v3 (Telegram API)
- gopkg.in/yaml.v3 (Config parsing)
- modernc.org/sqlite (Database)
- golang.org/x/sync (Concurrency utilities)

### Development
- golangci-lint (Linting)
- goimports (Import formatting)

## Configuration

- `config.yml.example` → `config.yml` (edit for local setup)
- Environment variables via `.env` file
- See SETUP.md for configuration details

## Testing

- Unit tests in `*_test.go` files
- Run: `go test ./...`
- Coverage: `go test -coverprofile=cov.out ./...` → `go tool cover -html=cov.out`

## Documentation

- `README.md` - Project overview
- `SETUP.md` - Installation and setup
- `.github/DEVELOPMENT.md` - Development guide
- `PROJECT_STRUCTURE.md` - This file
- `TODO.md` - Known issues and future work
