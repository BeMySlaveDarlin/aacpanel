ALTER TABLE actions ADD COLUMN detail text;

ALTER TABLE actions DROP CONSTRAINT actions_result_complete;
ALTER TABLE actions ADD CONSTRAINT actions_result_complete CHECK (
    (result IS NULL AND error IS NULL AND detail IS NULL AND duration_ms IS NULL AND finished_at IS NULL)
    OR (result IS NOT NULL AND finished_at IS NOT NULL)
);
