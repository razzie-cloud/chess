-- +goose Up
ALTER TABLE matches ADD COLUMN bot_level integer NOT NULL DEFAULT 0 CHECK (bot_level BETWEEN 0 AND 5);
CREATE TABLE match_bot_jobs (
    match_id uuid PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
    id uuid NOT NULL UNIQUE,
    revision bigint NOT NULL,
    pgn text NOT NULL,
    level integer NOT NULL CHECK (level BETWEEN 1 AND 5),
    retry_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX match_bot_jobs_retry ON match_bot_jobs(retry_at);

-- +goose Down
DROP TABLE match_bot_jobs;
ALTER TABLE matches DROP COLUMN bot_level;
