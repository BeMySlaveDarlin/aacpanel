CREATE TABLE brief_draft (
    brief_id text PRIMARY KEY,
    answers jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz
);
