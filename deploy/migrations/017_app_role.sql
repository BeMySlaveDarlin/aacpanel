DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'monitor_app') THEN
        RAISE NOTICE 'there is no role monitor_app — no rights are issued (see deploy/create-app-role.sh)';
        RETURN;
    END IF;
    EXECUTE 'GRANT CONNECT ON DATABASE ' || quote_ident(current_database()) || ' TO monitor_app';
    EXECUTE 'GRANT USAGE ON SCHEMA public TO monitor_app';

    EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO monitor_app';
    EXECUTE 'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO monitor_app';

    REVOKE ALL ON actions FROM monitor_app;
    GRANT SELECT, INSERT ON actions TO monitor_app;
    GRANT UPDATE (result, error, duration_ms, finished_at) ON actions TO monitor_app;
    EXECUTE 'GRANT USAGE, SELECT ON SEQUENCE actions_id_seq TO monitor_app';

    EXECUTE 'ALTER DEFAULT PRIVILEGES FOR ROLE ' || quote_ident(current_user) ||
            ' IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO monitor_app';
    EXECUTE 'ALTER DEFAULT PRIVILEGES FOR ROLE ' || quote_ident(current_user) ||
            ' IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO monitor_app';

    REVOKE INSERT, UPDATE, DELETE ON partition_config FROM monitor_app;

    EXECUTE 'GRANT EXECUTE ON FUNCTION ensure_partitions(interval) TO monitor_app';
    RAISE NOTICE 'the rights of monitor_app are issued';
END $$;

ALTER FUNCTION ensure_partitions(interval) SECURITY DEFINER;
