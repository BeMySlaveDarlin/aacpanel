CREATE TABLE usage_sessions (
    session_id uuid        PRIMARY KEY,
    contour    text        NOT NULL,
    cwd        text        NOT NULL DEFAULT '',
    git_branch text        NOT NULL DEFAULT '',
    version    text        NOT NULL DEFAULT '',
    started_at timestamptz,
    ended_at   timestamptz
);

CREATE TABLE usage_1h (
    session_id     uuid        NOT NULL REFERENCES usage_sessions (session_id) ON DELETE CASCADE,
    bucket         timestamptz NOT NULL,
    model          text        NOT NULL,
    agent          text        NOT NULL DEFAULT '',
    speed          text        NOT NULL DEFAULT '',
    service_tier   text        NOT NULL DEFAULT '',
    agent_kind     text        NOT NULL DEFAULT '',
    answers        integer     NOT NULL,
    input_tokens   bigint      NOT NULL DEFAULT 0,
    output_tokens  bigint      NOT NULL DEFAULT 0,
    cache_read     bigint      NOT NULL DEFAULT 0,
    cache_creation bigint      NOT NULL DEFAULT 0,
    cache_1h       bigint      NOT NULL DEFAULT 0,
    cache_5m       bigint      NOT NULL DEFAULT 0,
    iterations     integer     NOT NULL DEFAULT 0,
    thinking       integer     NOT NULL DEFAULT 0,
    latency_ms_sum bigint      NOT NULL DEFAULT 0,
    latency_ms_max integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, bucket, model, agent, speed, service_tier)
);

CREATE TABLE usage_events_1h (
    session_id      uuid        NOT NULL REFERENCES usage_sessions (session_id) ON DELETE CASCADE,
    bucket          timestamptz NOT NULL,
    agent           text        NOT NULL DEFAULT '',
    messages        integer     NOT NULL DEFAULT 0,
    compacts        integer     NOT NULL DEFAULT 0,
    interrupts      integer     NOT NULL DEFAULT 0,
    interrupts_tool integer     NOT NULL DEFAULT 0,
    api_errors      integer     NOT NULL DEFAULT 0,
    idle_lt_30s     integer     NOT NULL DEFAULT 0,
    idle_lt_2m      integer     NOT NULL DEFAULT 0,
    idle_lt_10m     integer     NOT NULL DEFAULT 0,
    idle_lt_1h      integer     NOT NULL DEFAULT 0,
    idle_lt_4h      integer     NOT NULL DEFAULT 0,
    idle_ge_4h      integer     NOT NULL DEFAULT 0,
    idle_ms_max     bigint      NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, bucket, agent)
);

CREATE TABLE usage_tools_1h (
    session_id uuid        NOT NULL REFERENCES usage_sessions (session_id) ON DELETE CASCADE,
    bucket     timestamptz NOT NULL,
    agent      text        NOT NULL DEFAULT '',
    tool       text        NOT NULL,
    calls      integer     NOT NULL,
    errors     integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, bucket, agent, tool)
);

CREATE TABLE usage_scan (
    path         text        PRIMARY KEY,
    contour      text        NOT NULL,
    session_id   uuid,
    inode        bigint      NOT NULL,
    size         bigint      NOT NULL,
    offset_bytes bigint      NOT NULL DEFAULT 0,
    head_sum     bytea,
    scanned_at   timestamptz NOT NULL DEFAULT now(),
    missing_at   timestamptz
);

CREATE INDEX usage_1h_bucket ON usage_1h USING brin (bucket);

CREATE INDEX usage_events_1h_bucket ON usage_events_1h USING brin (bucket);

CREATE INDEX usage_tools_1h_bucket ON usage_tools_1h USING brin (bucket);
