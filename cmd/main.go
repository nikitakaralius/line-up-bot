package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
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

// --- Storage ---

type Store struct {
	DB *sql.DB
}

func NewStore(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS polls (
			id SERIAL PRIMARY KEY,
			poll_id TEXT UNIQUE NOT NULL,
			chat_id BIGINT NOT NULL,
			message_id INT NOT NULL,
			topic TEXT NOT NULL,
			creator_id BIGINT NOT NULL,
			creator_username TEXT,
			creator_name TEXT,
			started_at TIMESTAMPTZ NOT NULL,
			duration_seconds INT NOT NULL,
			ends_at TIMESTAMPTZ NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			results_message_id INT,
			processed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS poll_votes (
			poll_id TEXT NOT NULL,
			user_id BIGINT NOT NULL,
			username TEXT,
			name TEXT,
			option_ids INT[] NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (poll_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS poll_results (
			poll_id TEXT PRIMARY KEY,
			results_text TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		)`,
	}
	for _, st := range stmts {
		if _, err := s.DB.ExecContext(ctx, st); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) InsertPoll(ctx context.Context, p *PollMeta) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO polls (
		poll_id, chat_id, message_id, topic, creator_id, creator_username, creator_name, started_at, duration_seconds, ends_at, status
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'active')
	ON CONFLICT (poll_id) DO NOTHING`,
		p.PollID, p.ChatID, p.MessageID, p.Topic, p.CreatorID, p.CreatorUsername, p.CreatorName, p.StartedAt, int(p.Duration/time.Second), p.EndsAt,
	)
	return err
}

func (s *Store) UpsertVote(ctx context.Context, pollID string, u tgbotapi.User, optionIDs []int) error {
	name := u.FirstName
	if u.LastName != "" {
		name = name + " " + u.LastName
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO poll_votes (poll_id, user_id, username, name, option_ids, updated_at)
	VALUES ($1,$2,$3,$4,$5, NOW())
	ON CONFLICT (poll_id, user_id) DO UPDATE SET username=EXCLUDED.username, name=EXCLUDED.name, option_ids=EXCLUDED.option_ids, updated_at=NOW()`,
		pollID, u.ID, u.UserName, name, IntSliceToArray(optionIDs),
	)
	return err
}

func IntSliceToArray(a []int) any {
	b := make([]int32, len(a))
	for i, v := range a {
		b[i] = int32(v)
	}
	return b
}

func (s *Store) FindExpiredActivePolls(ctx context.Context) ([]PollMeta, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT poll_id, chat_id, message_id, topic, ends_at FROM polls WHERE status='active' AND ends_at <= NOW()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []PollMeta
	for rows.Next() {
		var p PollMeta
		if err := rows.Scan(&p.PollID, &p.ChatID, &p.MessageID, &p.Topic, &p.EndsAt); err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, rows.Err()
}

func (s *Store) GetComingVoters(ctx context.Context, pollID string) ([]Voter, error) {
	// Option index 0 corresponds to "coming"
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id, COALESCE(username,''), COALESCE(name,'') FROM poll_votes WHERE poll_id=$1 AND 0 = ANY(option_ids)`, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var voters []Voter
	for rows.Next() {
		var v Voter
		if err := rows.Scan(&v.UserID, &v.Username, &v.Name); err != nil {
			return nil, err
		}
		voters = append(voters, v)
	}
	return voters, rows.Err()
}

func (s *Store) MarkProcessed(ctx context.Context, pollID string, resultsText string, resultsMessageID int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE polls SET status='processed', processed_at=NOW(), results_message_id=$2 WHERE poll_id=$1`, pollID, resultsMessageID)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO poll_results (poll_id, results_text, created_at) VALUES ($1,$2,NOW()) ON CONFLICT (poll_id) DO NOTHING`, pollID, resultsText)
	return err
}

// --- Bot ---

type PollMeta struct {
	PollID          string
	ChatID          int64
	MessageID       int
	Topic           string
	CreatorID       int64
	CreatorUsername string
	CreatorName     string
	StartedAt       time.Time
	Duration        time.Duration
	EndsAt          time.Time
}

type Voter struct {
	UserID   int64
	Username string
	Name     string
}

func main() {
	rand.Seed(time.Now().UnixNano())
	cfg := loadConfig()
	if cfg.BotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseDSN == "" {
		log.Fatal("POSTGRES_DSN is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := NewStore(cfg.DatabaseDSN)
	must(err)
	defer store.Close()
	must(waitForDB(ctx, store.DB))
	must(store.Migrate(ctx))

	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	must(err)
	if cfg.LogVerbose {
		bot.Debug = true
	}
	me := bot.Self.UserName
	log.Printf("Authorized on account @%s", me)

	// Start scheduler
	go schedulerLoop(ctx, bot, store)

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
				handleMessage(ctx, bot, store, update.Message, me)
			}
			if update.PollAnswer != nil {
				handlePollAnswer(ctx, store, update.PollAnswer)
			}
		}
	}
}

func handleMessage(ctx context.Context, bot *tgbotapi.BotAPI, store *Store, msg *tgbotapi.Message, botUsername string) {
	if msg.Chat == nil || (msg.Chat.Type != "group" && msg.Chat.Type != "supergroup") {
		return
	}
	text := msg.Text
	if text == "" {
		return
	}

	// Trigger on /poll command or mention of bot username
	triggered := false
	if msg.IsCommand() && msg.Command() == "poll" {
		triggered = true
		text = msg.CommandArguments()
	} else if len(msg.Entities) > 0 {
		for _, e := range msg.Entities {
			if e.Type == "mention" {
				mention := msg.Text[e.Offset : e.Offset+e.Length]
				if mention == "@"+botUsername {
					triggered = true
					// Strip mention from text
					text = msg.Text[e.Offset+e.Length:]
					break
				}
			}
		}
	}
	if !triggered {
		return
	}

	topic, dur, err := parseTopicAndDuration(text)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Usage: /poll Topic | 30m  (duration in Go format, e.g., 10m, 1h). Example: /poll Math practice | 45m")
		reply.ReplyToMessageID = msg.MessageID
		bot.Send(reply)
		return
	}

	// Create poll
	pollCfg := tgbotapi.NewPoll(msg.Chat.ID, topic, []string{"coming", "not coming"}...)
	pollCfg.IsAnonymous = false
	pollCfg.AllowsMultipleAnswers = false
	sent, err := bot.Send(pollCfg)
	if err != nil {
		log.Printf("send poll error: %v", err)
		return
	}
	if sent.Poll == nil {
		log.Printf("poll send returned no poll")
		return
	}
	p := &PollMeta{
		PollID:          sent.Poll.ID,
		ChatID:          msg.Chat.ID,
		MessageID:       sent.MessageID,
		Topic:           topic,
		CreatorID:       msg.From.ID,
		CreatorUsername: msg.From.UserName,
		CreatorName: msg.From.FirstName + func() string {
			if msg.From.LastName != "" {
				return " " + msg.From.LastName
			}
			return ""
		}(),
		StartedAt: time.Now().UTC(),
		Duration:  dur,
		EndsAt:    time.Now().UTC().Add(dur),
	}
	if err := store.InsertPoll(ctx, p); err != nil {
		log.Printf("insert poll error: %v", err)
	}
}

func parseTopicAndDuration(s string) (string, time.Duration, error) {
	// Expect format: "Topic | 30m" or "Topic 30m"
	// We'll split on '|' first; if not present, split by last space
	raw := s
	// Trim leading/trailing spaces
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, fmt.Errorf("empty input")
	}
	if i := strings.Index(raw, "|"); i >= 0 {
		topic := strings.TrimSpace(raw[:i])
		durStr := strings.TrimSpace(raw[i+1:])
		dur, err := time.ParseDuration(durStr)
		if err != nil || topic == "" {
			return "", 0, fmt.Errorf("bad format")
		}
		return topic, dur, nil
	}
	// No pipe, use last space
	lastSpace := strings.LastIndex(raw, " ")
	if lastSpace < 0 {
		return "", 0, fmt.Errorf("bad format")
	}
	topic := strings.TrimSpace(raw[:lastSpace])
	durStr := strings.TrimSpace(raw[lastSpace+1:])
	dur, err := time.ParseDuration(durStr)
	if err != nil || topic == "" {
		return "", 0, fmt.Errorf("bad format")
	}
	return topic, dur, nil
}

func handlePollAnswer(ctx context.Context, store *Store, pa *tgbotapi.PollAnswer) {
	// Persist vote
	_ = store.UpsertVote(ctx, pa.PollID, pa.User, pa.OptionIDs)
}

func shuffleVoters(v []Voter) {
	for i := range v {
		j := rand.Intn(i + 1)
		v[i], v[j] = v[j], v[i]
	}
}

func formatResults(topic string, voters []Voter) string {
	b := strings.Builder{}
	b.WriteString("Results for: ")
	b.WriteString(topic)
	b.WriteString("\n")
	if len(voters) == 0 {
		b.WriteString("No one is coming.")
		return b.String()
	}
	for i, v := range voters {
		b.WriteString(fmt.Sprintf("%d. ", i+1))
		if v.Username != "" {
			b.WriteString("@")
			b.WriteString(v.Username)
			if v.Name != "" {
				b.WriteString(" (")
				b.WriteString(v.Name)
				b.WriteString(")")
			}
		} else {
			if v.Name != "" {
				b.WriteString(v.Name)
			} else {
				b.WriteString("Anonymous")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func schedulerLoop(ctx context.Context, bot *tgbotapi.BotAPI, store *Store) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			polls, err := store.FindExpiredActivePolls(ctx)
			if err != nil {
				log.Printf("scheduler query error: %v", err)
				continue
			}
			for _, p := range polls {
				// Stop poll in chat
				stopCfg := tgbotapi.NewStopPoll(p.ChatID, p.MessageID)
				if _, err := bot.Send(stopCfg); err != nil {
					log.Printf("stop poll error: %v", err)
				}
				voters, err := store.GetComingVoters(ctx, p.PollID)
				if err != nil {
					log.Printf("get voters error: %v", err)
					continue
				}
				shuffleVoters(voters)
				text := formatResults(p.Topic, voters)
				msg := tgbotapi.NewMessage(p.ChatID, text)
				sent, err := bot.Send(msg)
				if err != nil {
					log.Printf("send results error: %v", err)
					continue
				}
				if err := store.MarkProcessed(ctx, p.PollID, text, sent.MessageID); err != nil {
					log.Printf("mark processed error: %v", err)
				}
			}
		}
	}
}

func waitForDB(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database not ready after timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
