ALTER TABLE sessions_raw ADD COLUMN session_id text;
ALTER TABLE sessions_raw ADD COLUMN cwd text;

ALTER TABLE sessions_1m ADD COLUMN session_id text;
ALTER TABLE sessions_1m ADD COLUMN cwd text;

ALTER TABLE sessions_1h ADD COLUMN session_id text;
ALTER TABLE sessions_1h ADD COLUMN cwd text;
