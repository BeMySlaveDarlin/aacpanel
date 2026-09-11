CREATE TABLE delivery_settings (
    device_id integer PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    quiet_from time,
    quiet_to   time,
    timezone text NOT NULL DEFAULT 'UTC',
    min_severity text NOT NULL DEFAULT 'info' CHECK (min_severity IN ('info', 'warning', 'critical')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT delivery_quiet_pair CHECK (
        (quiet_from IS NULL AND quiet_to IS NULL) OR (quiet_from IS NOT NULL AND quiet_to IS NOT NULL)
    ),
    CONSTRAINT delivery_quiet_span CHECK (quiet_from IS NULL OR quiet_from <> quiet_to)
);
