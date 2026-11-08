CREATE TABLE task_summaries (
    contour_id integer NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    fingerprint text NOT NULL,
    text text NOT NULL,
    model text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contour_id, period_start, period_end, fingerprint),
    CHECK (period_start < period_end)
);
