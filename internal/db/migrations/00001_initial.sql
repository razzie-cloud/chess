-- +goose Up
CREATE TABLE matches (
    id uuid PRIMARY KEY,
    creator_id uuid NOT NULL,
    creator_color text NOT NULL CHECK (creator_color IN ('w','b')),
    guest_name text NOT NULL CHECK (char_length(guest_name) BETWEEN 1 AND 80),
    mode text NOT NULL CHECK (mode IN ('standard','chess960','custom')),
    initial_fen text NOT NULL,
    current_fen text NOT NULL,
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','completed')),
    result text NOT NULL DEFAULT '*' CHECK (result IN ('*','1-0','0-1','1/2-1/2')),
    reason text NOT NULL DEFAULT '',
    tags jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    CHECK ((status = 'completed') = (result <> '*'))
);
CREATE INDEX matches_current_owner ON matches (creator_id, created_at DESC, id DESC) WHERE status IN ('pending','active');
CREATE INDEX matches_archive_owner ON matches (creator_id, completed_at DESC, id DESC) WHERE status = 'completed';

CREATE TABLE match_moves (
    match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    ply integer NOT NULL CHECK (ply > 0),
    uci text NOT NULL CHECK (uci ~ '^[a-h][1-8][a-h][1-8][qrbn]?$'),
    san text NOT NULL,
    fen text NOT NULL,
    actor text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (match_id, ply)
);
CREATE TABLE match_guest_links (
    match_id uuid PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
    token_digest text NOT NULL UNIQUE,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    seat text NOT NULL CHECK (seat IN ('w','b')),
    revoked boolean NOT NULL DEFAULT false
);
CREATE TABLE match_view_links (
    match_id uuid PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
    token text NOT NULL UNIQUE,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    revoked boolean NOT NULL DEFAULT false
);
CREATE TABLE guest_sessions (
    digest text PRIMARY KEY,
    match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    generation bigint NOT NULL CHECK (generation > 0),
    seat text NOT NULL CHECK (seat IN ('w','b')),
    expires_at timestamptz NOT NULL
);
CREATE INDEX guest_sessions_expiry ON guest_sessions (expires_at);
CREATE INDEX guest_sessions_match ON guest_sessions (match_id);
CREATE TABLE match_viewers (
    match_id uuid NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    viewer_id text NOT NULL,
    role text NOT NULL CHECK (role IN ('owner','guest','viewer')),
    scope text NOT NULL,
    seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (match_id, viewer_id)
);
CREATE INDEX match_viewers_expiry ON match_viewers (seen_at);

-- Creation receipts intentionally have no resource foreign key: deletion leaves
-- a minimal tombstone preventing delayed creation retries from recreating data.
CREATE TABLE mutation_receipts (
    actor text NOT NULL,
    resource text NOT NULL,
    request_id text NOT NULL,
    digest text NOT NULL,
    resource_id uuid,
    revision bigint NOT NULL,
    outcome text NOT NULL,
    PRIMARY KEY (actor, resource, request_id)
);
CREATE INDEX mutation_receipts_resource ON mutation_receipts (resource_id);

CREATE TABLE puzzle_collections (
    id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    published_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE puzzles (
    collection_id text NOT NULL REFERENCES puzzle_collections(id),
    slot smallint NOT NULL CHECK (slot BETWEEN 0 AND 255),
    initial_fen text NOT NULL,
    solver_color text NOT NULL CHECK (solver_color IN ('w','b')),
    mate_in integer NOT NULL CHECK (mate_in BETWEEN 1 AND 5),
    title text NOT NULL DEFAULT '',
    solution jsonb NOT NULL,
    content_digest text NOT NULL,
    fork_count integer NOT NULL CHECK (fork_count BETWEEN 0 AND 1),
    line_count integer NOT NULL CHECK (line_count BETWEEN 1 AND 3),
    PRIMARY KEY (collection_id, slot)
);
CREATE TABLE puzzle_attempts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    collection_id text NOT NULL,
    slot smallint NOT NULL,
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','solved','ended')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (collection_id, slot) REFERENCES puzzles(collection_id, slot)
);
CREATE INDEX puzzle_attempts_owner ON puzzle_attempts (owner_id, collection_id, slot, created_at DESC, id DESC);
CREATE TABLE puzzle_branches (
    attempt_id uuid NOT NULL REFERENCES puzzle_attempts(id) ON DELETE CASCADE,
    id text NOT NULL,
    parent_id text,
    path jsonb NOT NULL DEFAULT '[]',
    current_fen text NOT NULL,
    node_path text NOT NULL,
    moves_remaining integer NOT NULL CHECK (moves_remaining >= 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','expanded','solved')),
    PRIMARY KEY (attempt_id, id),
    FOREIGN KEY (attempt_id, parent_id) REFERENCES puzzle_branches(attempt_id, id)
);
CREATE TABLE user_puzzle_progress (
    user_id uuid NOT NULL,
    collection_id text NOT NULL REFERENCES puzzle_collections(id),
    solved bit(256) NOT NULL DEFAULT B'0'::bit(256),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, collection_id)
);

-- +goose Down
DROP TABLE user_puzzle_progress;
DROP TABLE puzzle_branches;
DROP TABLE puzzle_attempts;
DROP TABLE puzzles;
DROP TABLE puzzle_collections;
DROP TABLE mutation_receipts;
DROP TABLE match_viewers;
DROP TABLE guest_sessions;
DROP TABLE match_view_links;
DROP TABLE match_guest_links;
DROP TABLE match_moves;
DROP TABLE matches;
