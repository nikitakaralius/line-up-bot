CREATE TABLE IF NOT EXISTS poll_results
(
    poll_id      TEXT PRIMARY KEY,
    results_text TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL
);