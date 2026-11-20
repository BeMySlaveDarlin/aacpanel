ALTER TABLE session_restarts
    ADD COLUMN attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN cwd text NOT NULL DEFAULT '',
    ADD COLUMN parent text NOT NULL DEFAULT '',
    ADD COLUMN recovered_session text NOT NULL DEFAULT '';

UPDATE session_restarts r SET attempts = (
    SELECT count(*) FROM actions a
    WHERE a.kind = 'session.restart' AND a.device_name = 'context restart'
      AND a.params->>'conversation' = r.conversation AND a.ts >= r.commanded_at
) WHERE NOT r.done AND r.attempted_at IS NOT NULL;
