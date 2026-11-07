-- The last complete session census of each host boot, including conversations
-- that left it: a name reused by another process must not inherit its parent.
CREATE TABLE session_boots (
    id          bigserial PRIMARY KEY,
    host_id     integer NOT NULL REFERENCES hosts(id),
    boot_id     text NOT NULL CHECK (boot_id <> ''),
    snapshot_at timestamptz NOT NULL,
    claimed     boolean NOT NULL DEFAULT false,
    UNIQUE (host_id, boot_id)
);

CREATE TABLE session_living (
    host_id       integer NOT NULL REFERENCES hosts(id),
    boot_id       text NOT NULL,
    conversation  text NOT NULL CHECK (conversation <> ''),
    session       text NOT NULL,
    project_id    bigint NOT NULL DEFAULT 0,
    project       text NOT NULL DEFAULT '',
    contour       text NOT NULL DEFAULT '',
    config_dir    text NOT NULL DEFAULT '',
    cwd           text NOT NULL,
    transport     text NOT NULL DEFAULT '',
    agent         text NOT NULL,
    parent        text NOT NULL DEFAULT '',
    parent_conversation text NOT NULL DEFAULT '',
    outside       boolean NOT NULL,
    pause         jsonb,
    live          boolean NOT NULL DEFAULT true,
    seen_at       timestamptz NOT NULL,
    PRIMARY KEY (host_id, boot_id, conversation),
    FOREIGN KEY (host_id, boot_id) REFERENCES session_boots(host_id, boot_id)
);
CREATE INDEX session_living_conversation ON session_living(host_id, conversation, seen_at DESC);
