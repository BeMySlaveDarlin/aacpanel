-- What the person chose to hear in pushes: one row, the same on every device.
CREATE TABLE push_prefs (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    prefs jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);
