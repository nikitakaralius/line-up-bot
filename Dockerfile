FROM golang:1.25 AS bot_builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o lineup-bot-service cmd/service/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o lineup-bot-worker cmd/worker/main.go

# service
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=bot_builder /workspace/lineup-bot-service .
USER 65532:65532

ENV POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-service"]

# worker
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=bot_builder /workspace/lineup-bot-worker .
USER 65532:65532

ENV POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-worker"]