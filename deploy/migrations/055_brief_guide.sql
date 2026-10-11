CREATE TABLE brief_guide (
    id smallint PRIMARY KEY CHECK (id = 1),
    text text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
