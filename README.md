# Lineup Telegram Bot

A Telegram bot written in Go to manage weekly practice assignment queues via polls. It creates a poll with two options ("coming" / "not coming"), stores votes and metadata in PostgreSQL, and automatically posts a randomized lineup when the poll duration expires.

## Features
- /poll command or @mention to create a poll with topic and duration.
- Two options: coming, not coming (non-anonymous).
- PostgreSQL persistence (polls, votes, results) with auto-migrations.
- Background scheduler: closes expired polls, shuffles "coming" voters, and posts results.
- Dockerized with docker-compose for easy deployment.

## Prerequisites
- Telegram Bot Token (BotFather). Make the bot an admin in your group for best results.
- Docker and Docker Compose (or a local Go toolchain and PostgreSQL).

## Configuration
Environment variables:
- TELEGRAM_BOT_TOKEN: your Telegram bot token.
- POSTGRES_DSN: PostgreSQL DSN (example: postgres://user:pass@host:5432/db?sslmode=disable)
- LOG_VERBOSE: set to 1 for verbose logs.

## Usage
Add the bot to your Telegram group and promote to admin. Then:

- With command:
  /poll Topic | 30m
  /poll Math practice | 45m

- With @mention:
  @YourBotName Math practice | 45m

Duration uses Go format (e.g., 5m, 30m, 1h, 2h30m).

When the duration expires, the bot stops the poll and posts the randomized lineup of users who selected "coming":

1. @username (Telegram Name)
2. @username (Telegram Name)
...

## Run with Docker Compose
Export your token and start services:

TELEGRAM_BOT_TOKEN=YOUR_TOKEN_HERE docker compose up -d --build

db is a Postgres service; bot connects using the POSTGRES_DSN default in the Dockerfile. Logs:

docker compose logs -f bot

## Local Development
- Ensure PostgreSQL is running locally (or use docker-compose).
- Create the DB and user matching your POSTGRES_DSN or adjust Makefile run target.
- Build and run:

make run TELEGRAM_BOT_TOKEN=YOUR_TOKEN_HERE

## Schema Overview
- polls: metadata for each poll (topic, creator, start/duration, ends_at, status, references to messages).
- poll_votes: per-user answers with option indices (0 = coming, 1 = not coming).
- poll_results: cached result text for historical reference.

## Notes
- The bot uses long polling (getUpdates). For large groups, consider a webhook deployment.
- Ensure the bot has permission to create polls and send messages in the group.
- Privacy mode may need to be disabled if you want the bot to react to @mentions in groups.

## License
MIT
