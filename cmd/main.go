package main

import (
	"context"
	"flag"
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
	TelegramBotToken string
	DatabaseDSN      string
	LogVerbose       bool
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.TelegramBotToken, "telegram-bot-token", "", "Telegram bot token (required)")
	flag.StringVar(&cfg.DatabaseDSN, "dsn", "", "Postgres DB DSN (required)")
	flag.BoolVar(&cfg.LogVerbose, "verbose", false, "Enable verbose logging (default = false)")
	flag.Parse()

	if cfg.TelegramBotToken == "" {
		log.Fatal("telegram-bot-token is required")
	}
	if cfg.DatabaseDSN == "" {
		log.Fatal("dsn is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewStore(cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	err = telegram.WaitForDB(ctx, store.DB)
	if err != nil {
		log.Fatal(err)
	}

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
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
