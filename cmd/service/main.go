package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nikitkaralius/lineup/internal/polls"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/nikitkaralius/lineup/internal/storage"
	"github.com/nikitkaralius/lineup/internal/telegram"
)

// config holds environment configuration
type config struct {
	TelegramBotToken string
	DatabaseDSN      string
	LogVerbose       bool
	HTTPAddr         string
	WebhookURL       string
	Mode             string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.DatabaseDSN, "dsn", "", "Postgres DB DSN (required)")
	flag.BoolVar(&cfg.LogVerbose, "verbose", false, "Enable verbose logging (default = false)")
	flag.StringVar(&cfg.HTTPAddr, "http-addr", ":8080", "HTTP listen address (default :8080)")
	flag.StringVar(&cfg.WebhookURL, "webhook-url", "", "Telegram webhook public URL (required for webhook mode)")
	flag.StringVar(&cfg.Mode, "mode", "long-polling", "Bot update mode: long-polling or webhook (default long-polling)")
	flag.Parse()

	if cfg.DatabaseDSN == "" {
		log.Fatal("database dsn is required")
	}

	cfg.TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.TelegramBotToken == "" {
		log.Fatal("env TELEGRAM_BOT_TOKEN is required")
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

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("failed to create pgx pool: %v", err)
	}
	defer dbPool.Close()
	riverClient, err := river.NewClient(riverpgxv5.New(dbPool), &river.Config{})
	if err != nil {
		log.Fatalf("failed to create river client: %v", err)
	}
	pollsService := polls.NewPollsService(riverClient)

	mux := http.NewServeMux()

	switch cfg.Mode {
	case "webhook":
		if cfg.WebhookURL == "" {
			log.Fatal("webhook-url is required in webhook mode")
		}

		wh, err := tgbotapi.NewWebhook(cfg.WebhookURL)
		if err != nil {
			log.Fatalf("failed to build webhook: %v", err)
		}
		if _, err := bot.Request(wh); err != nil {
			log.Fatalf("failed to set webhook: %v", err)
		}
		info, err := bot.GetWebhookInfo()
		if err == nil {
			log.Printf("Webhook set: pending updates: %d", info.PendingUpdateCount)
		}

		mux.HandleFunc("POST /telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
			var update tgbotapi.Update
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if update.Message != nil {
				telegram.HandleMessage(r.Context(), bot, store, update.Message, me, pollsService)
			}
			if update.PollAnswer != nil {
				telegram.HandlePollAnswer(r.Context(), store, update.PollAnswer)
			}
			w.WriteHeader(http.StatusOK)
		})
	case "long-polling":
		if _, err := bot.Request(tgbotapi.DeleteWebhookConfig{}); err != nil {
			log.Printf("failed to remove webhook (continuing): %v", err)
		}

		u := tgbotapi.NewUpdate(0)
		u.Timeout = 30
		updates := bot.GetUpdatesChan(u)
		log.Printf("Started long polling with timeout=%d seconds", u.Timeout)
		for {
			select {
			case <-ctx.Done():
				bot.StopReceivingUpdates()
				return
			case update := <-updates:
				if update.Message != nil {
					telegram.HandleMessage(ctx, bot, store, update.Message, me, pollsService)
				}
				if update.PollAnswer != nil {
					telegram.HandlePollAnswer(ctx, store, update.PollAnswer)
				}
			}
		}
	default:
		log.Fatal("Unknown mode specified. See available options using --help")
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		log.Printf("Service listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-ctx.Done()
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctxShutdown)
}
