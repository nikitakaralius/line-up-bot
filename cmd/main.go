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

func main() {
	cfg := Config{
		BotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseDSN: os.Getenv("POSTGRES_DSN"),
		LogVerbose:  getenv("LOG_VERBOSE", "0") == "1",
	}
	if cfg.BotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseDSN == "" {
		log.Fatal("POSTGRES_DSN is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewStore(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	var err2 error = telegram.WaitForDB(ctx, store.DB)
	if err2 != nil {
		log.Fatal(err2)
	}
	var err3 error = store.Migrate(ctx)
	if err3 != nil {
		log.Fatal(err3)
	}

	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		log.Fatal(err)
	}
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
