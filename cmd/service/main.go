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
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/nikitkaralius/lineup/internal/async"
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
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.DatabaseDSN, "dsn", "", "Postgres DB DSN (required)")
	flag.BoolVar(&cfg.LogVerbose, "verbose", false, "Enable verbose logging (default = false)")
	flag.StringVar(&cfg.HTTPAddr, "http-addr", ":8080", "HTTP listen address (default :8080)")
	flag.StringVar(&cfg.WebhookURL, "webhook-url", "", "Telegram webhook public URL (required)")
	flag.Parse()

	if cfg.DatabaseDSN == "" {
		log.Fatal("dsn is required")
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

	if cfg.WebhookURL == "" {
		log.Fatal("webhook-url is required")
	}

	// Initialize River client (insert-only) and enqueuer
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("failed to create pgx pool: %v", err)
	}
	defer dbPool.Close()
	riverClient, err := river.NewClient(riverpgxv5.New(dbPool), &river.Config{})
	if err != nil {
		log.Fatalf("failed to create river client: %v", err)
	}
	enq := async.NewRiverEnqueuer(riverClient)
	defer enq.Close()

	// Configure webhook
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

	mux := http.NewServeMux()
	mux.HandleFunc("/telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var update tgbotapi.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if update.Message != nil {
			telegram.HandleMessage(r.Context(), bot, store, update.Message, me, enq)
		}
		if update.PollAnswer != nil {
			telegram.HandlePollAnswer(r.Context(), store, update.PollAnswer)
		}
		w.WriteHeader(http.StatusOK)
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
