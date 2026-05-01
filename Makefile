.PHONY: help lint test cover build clean tidy fmt dev

help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "} {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

# Development
dev:  ## Run bot locally
	go run ./cmd/bot

# Code quality
lint:  ## Run linters (fmt, vet, golangci-lint)
	gofmt -l -w .
	go vet ./...
	golangci-lint run --timeout=5m ./...

fmt:  ## Format code
	gofmt -s -w .
	goimports -w . 2>/dev/null || true

tidy:  ## Tidy dependencies
	go mod tidy

# Testing
test:  ## Run tests with race detection
	go test -race -v ./...

cover:  ## Generate test coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Build & Deployment
build:  ## Build Docker image (production)
	docker build -t hiddify-bot:latest .

clean:  ## Clean build artifacts
	rm -f coverage.out coverage.html
	go clean -testcache
