.PHONY: help dev lint fmt tidy test cover deploy clean

COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

# ── Local development ─────────────────────────────────────────────────────────

dev: ## Run bot locally via go run (no build step needed)
	go run -ldflags="-X main.commit=$(COMMIT)" ./cmd/bot

# ── Code quality ──────────────────────────────────────────────────────────────

lint: ## Run golangci-lint
	golangci-lint run --timeout=5m ./...

fmt: ## Format code
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || gofmt -s -w .

tidy: ## Tidy and verify dependencies
	go mod tidy && go mod verify

# ── Tests ─────────────────────────────────────────────────────────────────────

test: ## Run tests with race detector
	go test -race -count=1 ./...

cover: ## Run tests and open HTML coverage report
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

# ── Production ────────────────────────────────────────────────────────────────

build: ## Build the bot binary
	mkdir -p bin
	go build -ldflags="-X main.commit=$(COMMIT)" -o bin/hiddify-bot ./cmd/bot

build-linux: ## Build the bot binary for Linux
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-X main.commit=$(COMMIT)" -o bin/hiddify-bot ./cmd/bot

deploy: build ## Build and run the bot in background
	nohup ./bin/hiddify-bot > bot.log 2>&1 &

clean: ## Remove coverage artifacts, test cache and built binary
	rm -f coverage.out coverage.html bin/hiddify-bot bot.log
	go clean -testcache
