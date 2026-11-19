ALTER TABLE acts DROP CONSTRAINT acts_kind_check;
ALTER TABLE acts ADD CONSTRAINT acts_kind_check CHECK (kind IN ('do','secret','ask','call','claim'));
ALTER TABLE acts
    ADD COLUMN peer text,
    ADD COLUMN peer_conversation text,
    ADD COLUMN due_at timestamptz,
    ADD COLUMN rung smallint NOT NULL DEFAULT 0 CHECK (rung BETWEEN 0 AND 3),
    ADD COLUMN rung_at timestamptz,
    ADD CHECK ((kind = 'claim') = (peer IS NOT NULL));
CREATE INDEX acts_claims_due ON acts (due_at, id)
    WHERE kind='claim' AND state='open' AND due_at IS NOT NULL AND rung < 3;
CREATE INDEX acts_claims_peer ON acts (peer_conversation, created_at) WHERE kind='claim';
