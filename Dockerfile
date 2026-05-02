# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -X main.commit=${COMMIT}" \
    -o /out/hiddify-bot \
    ./cmd/bot

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21

# Run as non-root for security.
RUN addgroup -S app && adduser -S -G app app

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/hiddify-bot .

RUN mkdir -p data && chown -R app:app /app

USER app

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD pgrep hiddify-bot || exit 1

ENTRYPOINT ["./hiddify-bot"]
