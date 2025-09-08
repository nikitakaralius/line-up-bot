BINARY := lineup-bot
PKG := ./cmd/bot

.PHONY: all tidy build run clean docker-build up down logs ps

all: build

 tidy:
	go mod tidy

build: tidy
	GO111MODULE=on go build -o $(BINARY) $(PKG)

run: build
	TELEGRAM_BOT_TOKEN?=
	POSTGRES_DSN?=postgres://lineup:lineup@localhost:5432/lineup?sslmode=disable
	LOG_VERBOSE?=1
	TELEGRAM_BOT_TOKEN="$(TELEGRAM_BOT_TOKEN)" POSTGRES_DSN="$(POSTGRES_DSN)" LOG_VERBOSE=$(LOG_VERBOSE) ./$(BINARY)

clean:
	rm -f $(BINARY)

# Docker

docker-build:
	docker build -t lineup-bot:latest .

up:
	# Pass TELEGRAM_BOT_TOKEN via environment when calling: TELEGRAM_BOT_TOKEN=... make up
	docker compose up -d --build

down:
	docker compose down -v

logs:
	docker compose logs -f --tail=100

ps:
	docker compose ps
