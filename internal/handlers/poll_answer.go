package handlers

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nikitkaralius/lineup/internal/polls"
)

func HandlePollAnswer(ctx context.Context, store *polls.Repository, pa *tgbotapi.PollAnswer) {
	// Persist vote
	_ = store.UpsertVote(ctx, pa.PollID, pa.User, pa.OptionIDs)
}
