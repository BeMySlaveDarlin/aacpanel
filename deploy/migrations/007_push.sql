CREATE TABLE push_subscriptions (
    device_id  integer     PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    endpoint   text        NOT NULL,
    p256dh     bytea       NOT NULL CHECK (length(p256dh) = 65),
    auth       bytea       NOT NULL CHECK (length(auth) = 16),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_ok    timestamptz,
    last_error text,
    fails      integer     NOT NULL DEFAULT 0 CHECK (fails >= 0)
);

CREATE TABLE push_keys (
    id          integer     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    private_key bytea       NOT NULL,
    public_key  bytea       NOT NULL CHECK (length(public_key) = 65),
    created_at  timestamptz NOT NULL DEFAULT now()
);
