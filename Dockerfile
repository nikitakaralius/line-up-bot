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

ENV POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-service"]

# worker
FROM gcr.io/distroless/static:nonroot AS worker
WORKDIR /app
COPY --from=builder /workspace/lineup-bot-worker .
USER 65532:65532

ENV POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot-worker"]

# migrations
FROM alpine:3.22 AS migrations
WORKDIR /app

RUN apk add --no-cache ca-certificates bash

COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY migrations/ migrations/

ENV POSTGRES_DSN=""
