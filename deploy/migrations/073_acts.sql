CREATE TABLE acts (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    conversation text NOT NULL CHECK (conversation <> '' AND length(conversation) <= 128),
    session text NOT NULL CHECK (session <> '' AND length(session) <= 128),
    kind text NOT NULL CHECK (kind IN ('do', 'secret', 'ask', 'call')),
    blocking boolean NOT NULL DEFAULT false,
    title text NOT NULL CHECK (length(btrim(title)) > 0 AND length(title) <= 120),
    body jsonb NOT NULL DEFAULT '{}',
    state text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'answered', 'declined', 'withdrawn')),
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    answer jsonb,
    answered_by text NOT NULL DEFAULT '',
    CHECK ((closed_at IS NULL) = (state = 'open'))
);
CREATE INDEX acts_by_conversation ON acts (conversation, created_at);
CREATE INDEX acts_open ON acts (conversation, created_at) WHERE state = 'open';

INSERT INTO acts (id, conversation, session, kind, blocking, title, body, state, created_at, closed_at, answer, answered_by)
SELECT id, conversation, session, 'do', blocking, title, jsonb_build_object('steps', steps),
       CASE WHEN withdrawn_at IS NOT NULL THEN 'withdrawn' WHEN answer='done' THEN 'answered' WHEN answer='cant' THEN 'declined' ELSE 'open' END,
       created_at, coalesce(answered_at, withdrawn_at),
       CASE WHEN answered_at IS NOT NULL THEN jsonb_build_object('outcome', answer, 'note', note) END,
       CASE WHEN answered_at IS NOT NULL THEN 'person' WHEN withdrawn_at IS NOT NULL THEN session ELSE '' END
FROM session_requests;

INSERT INTO acts (id, conversation, session, kind, title, state, created_at, closed_at, answered_by)
SELECT md5(conversation || called_at::text), conversation, session, 'call', 'Called you',
       CASE WHEN seen_at IS NULL THEN 'open' ELSE 'answered' END, called_at, seen_at,
       CASE WHEN seen_at IS NULL THEN '' ELSE 'person' END
FROM session_calls;

-- The classifier queue has no session name or creation time. The conversation
-- remains a usable identity until a live snapshot supplies the current name.
INSERT INTO acts (id, conversation, session, kind, title, body, state, created_at, closed_at, answered_by)
SELECT md5(conversation || message), conversation, conversation, 'ask', coalesce(nullif(left(btrim(what),120),''),'Asks in text'),
       jsonb_build_object('source','prose','message',message,'what',what,'answers',answers),
       CASE WHEN answered THEN 'answered' WHEN closed THEN 'withdrawn' ELSE 'open' END,
       coalesce(started_at,now()), CASE WHEN answered OR closed THEN now() END,
       CASE WHEN answered THEN 'person' ELSE '' END
FROM text_questions WHERE verdict='yes';
