CREATE FUNCTION actions_no_truncate() RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'the action log cannot be changed: the table actions is not truncated'
        USING ERRCODE = 'restrict_violation',
              HINT = 'if the log really has to be wiped, that is done deliberately: DROP TRIGGER actions_no_truncate ON actions';
END;
$$;

CREATE TRIGGER actions_no_truncate
    BEFORE TRUNCATE ON actions
    FOR EACH STATEMENT EXECUTE FUNCTION actions_no_truncate();
