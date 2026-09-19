CREATE TABLE review_draft (
    id text PRIMARY KEY,
    session text NOT NULL,
    cwd text NOT NULL,
    base text NOT NULL DEFAULT '',
    notes jsonb NOT NULL DEFAULT '[]'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    path text NOT NULL DEFAULT ''
);

CREATE INDEX review_draft_session ON review_draft (session, updated_at DESC);
