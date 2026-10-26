-- Closures wait for ordinary news: a silent push alone can cost a browser's
-- subscription. The delivery claim is separate so waiting survives a restart.
ALTER TABLE text_questions
    ADD COLUMN what text NOT NULL DEFAULT '',
    ADD COLUMN closed boolean NOT NULL DEFAULT false,
    ADD COLUMN close_sent boolean NOT NULL DEFAULT false;
