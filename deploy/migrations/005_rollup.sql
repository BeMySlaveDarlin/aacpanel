CREATE TABLE rollup_state (
    name        text PRIMARY KEY,
    last_bucket timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    rows_last   bigint,
    took_ms     integer
);
