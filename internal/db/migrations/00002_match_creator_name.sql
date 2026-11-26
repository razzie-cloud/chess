-- +goose Up
ALTER TABLE matches ADD COLUMN creator_name text NOT NULL DEFAULT '' CHECK (char_length(creator_name) <= 64);

-- +goose Down
ALTER TABLE matches DROP COLUMN creator_name;
