-- A claim is kept before haiku starts, so a poll or a service restart cannot
-- spend another classification on the same message. No transcript text is kept.
CREATE TABLE text_questions (
    conversation text NOT NULL,
    message text NOT NULL,
    verdict text NOT NULL DEFAULT '' CHECK (verdict IN ('', 'pending', 'yes', 'no', 'error')),
    started_at timestamptz,
    pushed boolean NOT NULL DEFAULT false,
    PRIMARY KEY (conversation, message)
);
