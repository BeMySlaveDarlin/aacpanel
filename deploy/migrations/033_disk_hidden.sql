CREATE TABLE IF NOT EXISTS disk_hidden (
    path text PRIMARY KEY CHECK (path LIKE '/%'),
    hidden_at timestamptz NOT NULL DEFAULT now()
);

