#!/usr/bin/env bash
set -euo pipefail

: "${AACP_APP_PASSWORD:?set AACP_APP_PASSWORD (openssl rand -hex 24)}"

container="${AACP_DB_CONTAINER:-aacpanel-db}"
db="${AACP_DB_NAME:-aacpanel}"
owner="${AACP_DB_USER:-aacpanel}"
role="${AACP_APP_ROLE:-monitor_app}"

docker exec -i -e APP_PASSWORD="$AACP_APP_PASSWORD" "$container" \
    psql -v ON_ERROR_STOP=1 -U "$owner" -d "$db" <<EOF
-- The password arrives from the psql environment and goes into a session
-- setting: inside a \$\$ block psql substitution does not work, and building the
-- SQL by gluing strings with the password in is the easiest way to see it in
-- the logs later.
\getenv app_password APP_PASSWORD
-- The output is muted: SELECT would print the value that was set, that is the
-- password itself, into the terminal - and into a file as well when the output
-- of the script is recorded.
SELECT set_config('app.password', :'app_password', false) \g /dev/null

DO \$\$
DECLARE
    v_pass text := current_setting('app.password');
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$role') THEN
        EXECUTE format('ALTER ROLE %I LOGIN PASSWORD %L', '$role', v_pass);
        RAISE NOTICE 'the role $role was already there, the password is updated';
    ELSE
        EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', '$role', v_pass);
        RAISE NOTICE 'the role $role is created';
    END IF;

    -- The role gets its rights from the service at startup, not here: they are
    -- not secret and have to be applied on every run, not once by hand. Issue
    -- them here and the set would part from what the service knows, silently.
END
\$\$;

-- The setting lives only inside this session, but clear it explicitly.
SELECT set_config('app.password', '', false) \g /dev/null
EOF

echo
echo "The role $role is ready. Next:"
echo "  1. in .env: AACP_DB_DSN with the user $role and this password"
echo "  2. in .env: AACP_DB_MIGRATE_DSN with the user $owner (migrations)"
echo "  3. restart the service - at startup it issues the rights to the role"
