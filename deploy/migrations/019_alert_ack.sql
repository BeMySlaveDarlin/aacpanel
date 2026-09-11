ALTER TABLE alerts ADD COLUMN acknowledged_at timestamptz;

CREATE INDEX alerts_unacked ON alerts (id) WHERE closed_at IS NULL AND acknowledged_at IS NULL;
