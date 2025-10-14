package handlers

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nikitkaralius/lineup/internal/voters"
)

func HandlePollAnswer(ctx context.Context, store *voters.Repository, pa *tgbotapi.PollAnswer) {
	// Persist vote
	_ = store.UpsertVote(ctx, pa.PollID, pa.User, pa.OptionIDs)
}
