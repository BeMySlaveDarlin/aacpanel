CREATE TABLE session_views (
    device_id integer     PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    session   text        NOT NULL CHECK (session <> '' AND length(session) <= 128),
    seen_at   timestamptz NOT NULL DEFAULT now()
);
