package polls

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nikitkaralius/lineup/internal/voters"
	"github.com/riverqueue/river"
)

// FinishPollArgs defines the arguments for a job that finalizes a Telegram poll
// by stopping it and posting the results.
// This type is shared between service (for enqueue) and worker (for processing).
type FinishPollArgs struct {
	PollID    string `json:"poll_id"`
	ChatID    int64  `json:"chat_id"`
	MessageID int    `json:"message_id"`
	Topic     string `json:"topic"`
}

// Kind implements river.JobArgs to identify this job type.
func (FinishPollArgs) Kind() string { return "finish_poll" }

type FinishPollWorker struct {
	river.WorkerDefaults[FinishPollArgs]
	store *Repository
	bot   *tgbotapi.BotAPI
}

func NewFinishPollWorker(store *Repository, bot *tgbotapi.BotAPI) *FinishPollWorker {
	return &FinishPollWorker{store: store, bot: bot}
}

func (w *FinishPollWorker) Work(ctx context.Context, job *river.Job[FinishPollArgs]) error {
	args := job.Args
	// Stop poll in chat
	stopCfg := tgbotapi.NewStopPoll(args.ChatID, args.MessageID)
	if _, err := w.bot.Send(stopCfg); err != nil {
		log.Printf("stop poll error: %v", err)
		// keep going; maybe already stopped
	}
	voters, err := w.store.GetComingVoters(ctx, args.PollID)
	if err != nil {
		return err
	}
	shuffleVoters(voters)
	text := formatResults(args.Topic, voters)
	msg := tgbotapi.NewMessage(args.ChatID, text)
	sent, err := w.bot.Send(msg)
	if err != nil {
		return err
	}
	if err := w.store.MarkProcessed(ctx, args.PollID, text, sent.MessageID); err != nil {
		return err
	}
	return nil
}

func shuffleVoters(v []voters.TelegramVoterDTO) {
	for i := range v {
		j := rand.Intn(i + 1)
		v[i], v[j] = v[j], v[i]
	}
}

func formatResults(topic string, voters []voters.TelegramVoterDTO) string {
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
