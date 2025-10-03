# build
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

RUN go install github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.0

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o lineup-bot-service cmd/service/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o lineup-bot-worker cmd/worker/main.go

# service
FROM gcr.io/distroless/static:nonroot AS service
WORKDIR /app
COPY --from=builder /workspace/lineup-bot-service .
USER 65532:65532

ENV LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-service"]
CMD ["-telegram-bot-token", "env:TELEGRAM_BOT_TOKEN", "-dsn", "env:POSTGRES_DSN", "-verbose", "env:LOG_VERBOSE"]

# worker
FROM gcr.io/distroless/static:nonroot AS worker
WORKDIR /app
COPY --from=builder /workspace/lineup-bot-worker .
USER 65532:65532

ENV LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-worker"]
CMD ["-telegram-bot-token", "env:TELEGRAM_BOT_TOKEN", "-dsn", "env:POSTGRES_DSN", "-verbose", "env:LOG_VERBOSE"]

# migrations
FROM gcr.io/distroless/static:nonroot AS migrations
WORKDIR /app

COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY migrations/ migrations/

ENV POSTGRES_DSN=""
ENTRYPOINT ["migrate"]
CMD ["-path", "/app/migrations", "-database", "env:POSTGRES_DSN", "up"]
