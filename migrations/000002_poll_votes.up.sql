CREATE TABLE IF NOT EXISTS poll_votes
(
    poll_id    TEXT        NOT NULL,
    user_id    BIGINT      NOT NULL,
    username   TEXT,
    name       TEXT,
    option_ids INT[]       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (poll_id, user_id)
);