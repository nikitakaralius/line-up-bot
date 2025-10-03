FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd/main.go cmd/main.go

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o line-up-bot cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /workspace/line-up-bot .
USER 655532:65532

ENV POSTGRES_DSN="postgres://lineup:lineup@db:5432/lineup?sslmode=disable" \
    LOG_VERBOSE=0

ENTRYPOINT ["/app/lineup-bot"]