-- A sleeping conversation is absent from the host census; its pause belongs
-- to the panel rather than to a process that can disappear with the host.
CREATE TABLE session_pauses (
    host_id      integer NOT NULL REFERENCES hosts(id),
    conversation text NOT NULL,
    session      text NOT NULL,
    saved        jsonb NOT NULL,
    snapshot     jsonb NOT NULL,
    until_at     timestamptz,
    sleeping     boolean NOT NULL DEFAULT false,
    commanded_at timestamptz NOT NULL,
    PRIMARY KEY (host_id, conversation),
    UNIQUE (host_id, session)
);
