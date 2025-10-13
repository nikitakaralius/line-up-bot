package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nikitkaralius/lineup/internal/async"
	"github.com/nikitkaralius/lineup/internal/jobs"
	"github.com/nikitkaralius/lineup/internal/models"
	"github.com/nikitkaralius/lineup/internal/storage"
)

func HandleMessage(ctx context.Context, bot *tgbotapi.BotAPI, store *storage.Store, msg *tgbotapi.Message, botUsername string, enq async.Enqueuer) {
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
	p := &models.PollMeta{
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
		return
	}
	// Enqueue async job to finalize poll at EndsAt
	if enq != nil {
		args := jobs.FinishPollArgs{PollID: p.PollID, ChatID: p.ChatID, MessageID: p.MessageID, Topic: p.Topic}
		if err := enq.EnqueueFinishPoll(ctx, args, p.EndsAt); err != nil {
			log.Printf("enqueue finish poll error: %v", err)
		}
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

func HandlePollAnswer(ctx context.Context, store *storage.Store, pa *tgbotapi.PollAnswer) {
	// Persist vote
	_ = store.UpsertVote(ctx, pa.PollID, pa.User, pa.OptionIDs)
}

func shuffleVoters(v []models.Voter) {
	for i := range v {
		j := rand.Intn(i + 1)
		v[i], v[j] = v[j], v[i]
	}
}

func formatResults(topic string, voters []models.Voter) string {
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

func SchedulerLoop(ctx context.Context, bot *tgbotapi.BotAPI, store *storage.Store) {
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

func WaitForDB(ctx context.Context, db *sql.DB) error {
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
