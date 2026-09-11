ALTER TABLE devices ADD COLUMN revoked_at timestamptz;

ALTER TABLE devices ALTER COLUMN credential_id DROP NOT NULL;
ALTER TABLE devices ALTER COLUMN public_key    DROP NOT NULL;
ALTER TABLE devices ADD CONSTRAINT devices_revoked_has_no_key CHECK (
    (revoked_at IS NULL AND credential_id IS NOT NULL AND public_key IS NOT NULL)
    OR
    (revoked_at IS NOT NULL AND credential_id IS NULL AND public_key IS NULL)
);

CREATE INDEX devices_live ON devices (id) WHERE revoked_at IS NULL;
