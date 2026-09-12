ALTER TABLE rules ADD COLUMN key text;

UPDATE rules SET key = CASE
    WHEN subject = 'container.cpu_pct'   THEN 'container.cpu'
    WHEN subject = 'container.mem_pct'   THEN 'container.memory'
    WHEN subject = 'container.unhealthy' THEN 'container.unhealthy'
    WHEN subject = 'host.cpu_pct'        THEN 'host.cpu'
    WHEN subject = 'host.mem_pct'        THEN 'host.memory'
    WHEN subject = 'host.swap_bytes'     THEN 'host.swap'
    WHEN subject = 'host.load1'          THEN 'host.load'
    WHEN subject = 'disk.used_pct'       THEN CASE WHEN threshold >= 95 THEN 'disk.full' ELSE 'disk.filling' END
    WHEN subject = 'session.pct'         THEN 'session.limit'
    WHEN subject = 'stack.running_pct'   THEN 'stack.down'
    WHEN subject = 'probe.fail_streak'   THEN 'probe.failing'
END;

UPDATE rules SET key = subject || '.' || id WHERE key IS NULL;

UPDATE rules r SET key = r.key || '.' || r.id
 WHERE EXISTS (SELECT 1 FROM rules o WHERE o.key = r.key AND o.id < r.id);

ALTER TABLE rules ALTER COLUMN key SET NOT NULL;
ALTER TABLE rules ADD CONSTRAINT rules_key_key UNIQUE (key);

ALTER TABLE rules DROP CONSTRAINT rules_name_key;

ALTER TABLE probes ADD COLUMN key text;
ALTER TABLE probes ADD CONSTRAINT probes_key_key UNIQUE (key);
ALTER TABLE probes ADD CONSTRAINT probes_key_service
    CHECK (key IS NULL OR runner = 'service');

UPDATE probes SET key = CASE name
    WHEN 'API Anthropic'    THEN 'api.anthropic'
    WHEN 'API Google'       THEN 'api.google'
    WHEN 'API Telegram'     THEN 'api.telegram'
    WHEN 'API OpenAI'       THEN 'api.openai'
    WHEN 'Anthropic status' THEN 'status.anthropic'
    WHEN 'OpenAI status'    THEN 'status.openai'
END
WHERE runner = 'service';
