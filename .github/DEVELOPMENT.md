# Development Guide

## Setup

```bash
git clone <repo>
cd hiddify-bot
go mod download
```

## Local Development

```bash
go run ./cmd/bot                  # Run locally
go test ./...                     # Run tests
go test -v -coverprofile=cov.out # With coverage
go tool cover -html=cov.out       # View coverage in browser
```

## Code Quality

```bash
gofmt -w .                        # Format code
go vet ./...                      # Run vet checks
golangci-lint run ./...           # Run all linters
```

## Docker Build

```bash
docker build -t hiddify-bot:latest .
docker run hiddify-bot:latest
```

## Making Changes

1. Create a branch: `git checkout -b feature/xxx`
2. Make changes and run tests: `go test ./...`
3. Format code: `gofmt -w .`
4. Run linters: `golangci-lint run ./...`
5. Commit and push
6. Create PR - CI will run automatically

## CI/CD Pipeline

- **On PR/Push**: `lint` → `test` → `build`
- **On Main/Tags**: Docker image is verified
- All checks must pass before merging

## Troubleshooting

### Tests fail on Windows
Windows doesn't support `-race` flag. Use: `go test ./...`

### golangci-lint not found
Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`

### goimports not found
Install: `go install golang.org/x/tools/cmd/goimports@latest`
