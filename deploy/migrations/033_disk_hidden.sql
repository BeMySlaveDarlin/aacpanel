CREATE TABLE IF NOT EXISTS disk_hidden (
    path text PRIMARY KEY CHECK (path LIKE '/%'),
    hidden_at timestamptz NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'monitor_app') THEN
        RAISE NOTICE 'there is no role monitor_app — no rights are issued (see deploy/create-app-role.sh)';
        RETURN;
    END IF;
    GRANT SELECT, INSERT, DELETE ON disk_hidden TO monitor_app;
END
$$;
