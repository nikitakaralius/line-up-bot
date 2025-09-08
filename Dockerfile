# syntax=docker/dockerfile:1

# ---- Builder ----
FROM golang:1.24-alpine AS builder
WORKDIR /src

# Install build deps
RUN apk add --no-cache git ca-certificates tzdata

# Cache deps
COPY go.mod ./
# go.sum may not exist yet, so ignore; tidy will create it in build stage
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download || true

# Copy source
COPY . .

# Build
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/lineup-bot ./cmd/bot

# ---- Runner ----
FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/lineup-bot /app/lineup-bot

ENV TELEGRAM_BOT_TOKEN="" \
    POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot"]
