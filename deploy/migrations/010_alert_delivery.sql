ALTER TABLE alerts
    ADD COLUMN notified_at       timestamptz,
    ADD COLUMN notified_severity text;

CREATE INDEX alerts_open ON alerts (rule_id) WHERE closed_at IS NULL;
