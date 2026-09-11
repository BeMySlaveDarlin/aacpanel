DROP TABLE delivery_settings;

ALTER TABLE alerts
    DROP COLUMN notified_at,
    DROP COLUMN notified_severity;

CREATE TABLE push_state (
    key text PRIMARY KEY,
    event jsonb NOT NULL,
    raised_at timestamptz NOT NULL DEFAULT now()
);
