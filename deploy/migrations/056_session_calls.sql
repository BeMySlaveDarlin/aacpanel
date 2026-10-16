-- The last call of each conversation for the person, and whether they have
-- opened the session since: the lists mark the session until they do.
CREATE TABLE session_calls (
    conversation text        PRIMARY KEY CHECK (conversation <> '' AND length(conversation) <= 128),
    session      text        NOT NULL CHECK (session <> '' AND length(session) <= 128),
    called_at    timestamptz NOT NULL,
    seen_at      timestamptz NULL
);

CREATE INDEX session_calls_by_session ON session_calls (session);
