ALTER TABLE session_restarts ADD COLUMN finish text NOT NULL DEFAULT 'restart'
    CHECK (finish IN ('restart', 'close', 'finalize'));

CREATE TABLE session_finalizations (
    conversation text PRIMARY KEY CHECK (conversation <> ''),
    ready_at timestamptz NOT NULL
);
