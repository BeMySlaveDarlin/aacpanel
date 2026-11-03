ALTER TABLE text_questions ADD COLUMN answered boolean NOT NULL DEFAULT false;

-- Claim a letter before delivery: a repeated snapshot or service restart
-- must not mail a parent twice about the same question.
CREATE TABLE parent_questions (
    conversation text NOT NULL,
    question text NOT NULL,
    parent text NOT NULL,
    due timestamptz NOT NULL,
    delivered boolean NOT NULL DEFAULT false,
    answered boolean NOT NULL DEFAULT false,
    PRIMARY KEY (conversation, question)
);
