package polls

import (
	"context"
	"database/sql"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nikitkaralius/lineup/internal/voters"
)

// This AI crap will be refactored

type Repository struct {
	DB *sql.DB
}

func NewRepository(dsn string) (*Repository, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Repository{DB: db}, nil
}

func (s *Repository) Close() error { return s.DB.Close() }

func (s *Repository) InsertPoll(ctx context.Context, p *TelegramPollDTO) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO polls (
		poll_id, chat_id, message_id, topic, creator_id, creator_username, creator_name, started_at, duration_seconds, ends_at, status
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'active')
	ON CONFLICT (poll_id) DO NOTHING`,
		p.PollID, p.ChatID, p.MessageID, p.Topic, p.CreatorID, p.CreatorUsername, p.CreatorName, p.StartedAt, int(p.Duration/time.Second), p.EndsAt,
	)
	return err
}

func (s *Repository) UpsertVote(ctx context.Context, pollID string, u tgbotapi.User, optionIDs []int) error {
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

func (s *Repository) FindExpiredActivePolls(ctx context.Context) ([]TelegramPollDTO, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT poll_id, chat_id, message_id, topic, ends_at FROM polls WHERE status='active' AND ends_at <= NOW()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []TelegramPollDTO
	for rows.Next() {
		var p TelegramPollDTO
		if err := rows.Scan(&p.PollID, &p.ChatID, &p.MessageID, &p.Topic, &p.EndsAt); err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, rows.Err()
}

func (s *Repository) GetComingVoters(ctx context.Context, pollID string) ([]voters.TelegramVoterDTO, error) {
	// Option index 0 corresponds to "coming"
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id, COALESCE(username,''), COALESCE(name,'') FROM poll_votes WHERE poll_id=$1 AND 0 = ANY(option_ids)`, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vs []voters.TelegramVoterDTO
	for rows.Next() {
		var v voters.TelegramVoterDTO
		if err := rows.Scan(&v.UserID, &v.Username, &v.Name); err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	return vs, rows.Err()
}

func (s *Repository) MarkProcessed(ctx context.Context, pollID string, resultsText string, resultsMessageID int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE polls SET status='processed', processed_at=NOW(), results_message_id=$2 WHERE poll_id=$1`, pollID, resultsMessageID)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO poll_results (poll_id, results_text, created_at) VALUES ($1,$2,NOW()) ON CONFLICT (poll_id) DO NOTHING`, pollID, resultsText)
	return err
}
