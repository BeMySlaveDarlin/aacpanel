DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'monitor_app') THEN
        RAISE NOTICE 'there is no role monitor_app — no rights are issued (see deploy/create-app-role.sh)';
        RETURN;
    END IF;
    GRANT UPDATE (detail) ON actions TO monitor_app;
    RAISE NOTICE 'monitor_app got the right to append detail';
END $$;
