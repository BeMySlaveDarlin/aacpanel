DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'monitor_app') THEN
        RAISE NOTICE 'there is no role monitor_app — there is nothing to take away (see deploy/create-app-role.sh)';
        RETURN;
    END IF;
    REVOKE UPDATE ON disk_hidden FROM monitor_app;
END
$$;
