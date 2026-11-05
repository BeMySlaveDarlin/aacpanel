-- When the person last had each conversation on screen, on any device: a
-- turn that ended after it is one they have not seen.
CREATE TABLE session_seen (
    conversation text        PRIMARY KEY CHECK (conversation <> '' AND length(conversation) <= 128),
    seen_at      timestamptz NOT NULL
);

-- What the card of a live conversation last showed — its checklist step, the
-- agent and how it was run — kept for the archive card once the session is
-- gone: the transcript says none of it.
CREATE TABLE session_last (
    conversation text        PRIMARY KEY CHECK (conversation <> '' AND length(conversation) <= 128),
    last         jsonb       NOT NULL,
    kept_at      timestamptz NOT NULL DEFAULT now()
);
