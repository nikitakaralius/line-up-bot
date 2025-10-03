CREATE TABLE IF NOT EXISTS polls
(
    id                 SERIAL PRIMARY KEY,
    poll_id            TEXT UNIQUE NOT NULL,
    chat_id            BIGINT      NOT NULL,
    message_id         INT         NOT NULL,
    topic              TEXT        NOT NULL,
    creator_id         BIGINT      NOT NULL,
    creator_username   TEXT,
    creator_name       TEXT,
    started_at         TIMESTAMPTZ NOT NULL,
    duration_seconds   INT         NOT NULL,
    ends_at            TIMESTAMPTZ NOT NULL,
    status             TEXT        NOT NULL DEFAULT 'active',
    results_message_id INT,
    processed_at       TIMESTAMPTZ
);