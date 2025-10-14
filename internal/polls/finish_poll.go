package polls

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
