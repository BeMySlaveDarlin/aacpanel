-- The map journal cannot restore a registry whose only copy is in Postgres.
-- The project row lock also serializes this check with registry writers.
CREATE FUNCTION keep_panel_task_registry() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    tasks bigint;
    handoffs bigint;
BEGIN
    IF OLD.task_writer = 'panel' THEN
        SELECT count(*) INTO tasks FROM project_tasks WHERE project_id = OLD.id;
        SELECT count(*) INTO handoffs FROM project_handoffs WHERE project_id = OLD.id;
        IF tasks > 0 OR handoffs > 0 THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                CONSTRAINT = 'panel_task_registry_not_empty',
                MESSAGE = format('project "%s" holds %s tasks and %s handoffs in the panel; its task registry must be kept',
                                 OLD.name, tasks, handoffs);
        END IF;
    END IF;
    RETURN OLD;
END;
$$;

CREATE TRIGGER keep_panel_task_registry
    BEFORE DELETE ON profile_projects
    FOR EACH ROW EXECUTE FUNCTION keep_panel_task_registry();
