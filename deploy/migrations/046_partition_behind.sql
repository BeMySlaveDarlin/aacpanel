-- A partition is made for the period in progress and for the one just ended.
--
-- A row can be stamped a moment before a period ends and written a moment
-- after it: with only the period in progress on disk, that row has nowhere to
-- go and the write fails. The period just ended costs an empty table until
-- something lands in it, and retention takes it away on the same terms as any
-- other.
CREATE OR REPLACE FUNCTION ensure_partitions(p_ahead interval DEFAULT '7 days')
RETURNS integer
LANGUAGE plpgsql
SET timezone = 'UTC'
SET search_path = public, pg_temp
AS $$
DECLARE
    cfg    record;
    v_step interval;
    v_from timestamptz;
    v_to   timestamptz;
    v_name text;
    v_made integer := 0;
BEGIN
    FOR cfg IN SELECT relname, step_unit FROM partition_config ORDER BY relname LOOP
        v_step := ('1 ' || cfg.step_unit)::interval;
        v_from := date_trunc(cfg.step_unit, now()) - v_step;
        WHILE v_from <= now() + p_ahead LOOP
            v_to   := v_from + v_step;

            v_name := cfg.relname || '_' || to_char(v_from, 'YYYYMMDD');
            IF to_regclass('public.' || quote_ident(v_name)) IS NULL THEN
                EXECUTE format(
                    'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                    v_name, cfg.relname, v_from, v_to);
                v_made := v_made + 1;
            END IF;
            v_from := v_to;
        END LOOP;
    END LOOP;
    RETURN v_made;
END;
$$;

SELECT ensure_partitions('7 days');
