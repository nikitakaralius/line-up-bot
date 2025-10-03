package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nikitkaralius/lineup/internal/storage"
	"github.com/nikitkaralius/lineup/internal/telegram"
)

// Config holds environment configuration
type Config struct {
	BotToken    string
	DatabaseDSN string
	LogVerbose  bool
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() Config {
	return Config{
		BotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseDSN: os.Getenv("POSTGRES_DSN"),
		LogVerbose:  getenv("LOG_VERBOSE", "0") == "1",
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	cfg := loadConfig()
	if cfg.BotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseDSN == "" {
		log.Fatal("POSTGRES_DSN is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewStore(cfg.DatabaseDSN)
	must(err)
	defer store.Close()
	must(telegram.WaitForDB(ctx, store.DB))
	must(store.Migrate(ctx))

	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	must(err)
	if cfg.LogVerbose {
		bot.Debug = true
	}
	me := bot.Self.UserName
	log.Printf("Authorized on account @%s", me)

	// Start scheduler
	go telegram.SchedulerLoop(ctx, bot, store)

	// Start updates
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down...")
			return
		case update := <-updates:
			if update.Message != nil {
				telegram.HandleMessage(ctx, bot, store, update.Message, me)
			}
			if update.PollAnswer != nil {
				telegram.HandlePollAnswer(ctx, store, update.PollAnswer)
			}
		}
	}
}
