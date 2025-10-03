package models

import "time"

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
