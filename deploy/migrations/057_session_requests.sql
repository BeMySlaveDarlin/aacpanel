-- Tasks for the person's hands stay open until answered or withdrawn, even
-- when the conversation has been viewed on another device.
CREATE TABLE session_requests (
    id           text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    conversation text NOT NULL CHECK (conversation <> '' AND length(conversation) <= 128),
    session      text NOT NULL CHECK (session <> '' AND length(session) <= 128),
    title        text NOT NULL CHECK (length(btrim(title)) > 0 AND length(title) <= 120),
    steps        text NOT NULL CHECK (length(btrim(steps)) > 0 AND length(steps) <= 2000),
    blocking     boolean NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    answered_at  timestamptz,
    withdrawn_at timestamptz,
    answer       text NOT NULL DEFAULT '' CHECK (answer IN ('', 'done', 'cant')),
    note         text NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    CHECK ((answered_at IS NULL) = (answer = '')),
    CHECK (answered_at IS NULL OR withdrawn_at IS NULL)
);
CREATE INDEX session_requests_by_conversation ON session_requests (conversation, created_at);
