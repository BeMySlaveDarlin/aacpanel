ALTER TABLE probes DROP CONSTRAINT probes_kind_check;
ALTER TABLE probes ADD CONSTRAINT probes_kind_check CHECK (kind IN ('tcp', 'http', 'wg'));
