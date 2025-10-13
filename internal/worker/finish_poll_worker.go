package worker

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/riverqueue/river"

	"github.com/nikitkaralius/lineup/internal/jobs"
	"github.com/nikitkaralius/lineup/internal/models"
	"github.com/nikitkaralius/lineup/internal/storage"
)

type FinishPollWorker struct {
	river.WorkerDefaults[jobs.FinishPollArgs]
	store *storage.Store
	bot   *tgbotapi.BotAPI
}

func NewFinishPollWorker(store *storage.Store, bot *tgbotapi.BotAPI) *FinishPollWorker {
	return &FinishPollWorker{store: store, bot: bot}
}

func (w *FinishPollWorker) Work(ctx context.Context, job *river.Job[jobs.FinishPollArgs]) error {
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
