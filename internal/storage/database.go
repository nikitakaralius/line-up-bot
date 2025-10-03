package storage

import (
	"context"
	"database/sql"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nikitkaralius/lineup/internal/models"
)

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

func (s *Store) InsertPoll(ctx context.Context, p *models.PollMeta) error {
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

func (s *Store) FindExpiredActivePolls(ctx context.Context) ([]models.PollMeta, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT poll_id, chat_id, message_id, topic, ends_at FROM polls WHERE status='active' AND ends_at <= NOW()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []models.PollMeta
	for rows.Next() {
		var p models.PollMeta
		if err := rows.Scan(&p.PollID, &p.ChatID, &p.MessageID, &p.Topic, &p.EndsAt); err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, rows.Err()
}

func (s *Store) GetComingVoters(ctx context.Context, pollID string) ([]models.Voter, error) {
	// Option index 0 corresponds to "coming"
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id, COALESCE(username,''), COALESCE(name,'') FROM poll_votes WHERE poll_id=$1 AND 0 = ANY(option_ids)`, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var voters []models.Voter
	for rows.Next() {
		var v models.Voter
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
