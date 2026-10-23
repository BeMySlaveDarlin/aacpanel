CREATE TABLE session_restarts (
    conversation text PRIMARY KEY,
    session text NOT NULL,
    commanded_at timestamptz NOT NULL,
    commanded boolean NOT NULL DEFAULT false,
    reminded_at timestamptz,
    ready_at timestamptz,
    intent text NOT NULL DEFAULT '',
    attempted_at timestamptz,
    done boolean NOT NULL DEFAULT false
);
