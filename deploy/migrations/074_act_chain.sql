ALTER TABLE acts DROP CONSTRAINT acts_state_check;
ALTER TABLE acts DROP CONSTRAINT acts_check;
ALTER TABLE acts ADD CONSTRAINT acts_state_check CHECK (state IN ('open','answered','declined','withdrawn','failed'));
ALTER TABLE acts ADD CHECK ((closed_at IS NULL) = (state IN ('open','failed')));
ALTER TABLE acts ADD COLUMN step int CHECK (step BETWEEN 1 AND 10);
ALTER TABLE acts ADD COLUMN outputs jsonb;
-- A saved notice is retried with its own message id after a delivery failure.
ALTER TABLE acts ADD COLUMN notice jsonb;
ALTER TABLE acts ADD COLUMN notified int NOT NULL DEFAULT 0;
