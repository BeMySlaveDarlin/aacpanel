ALTER TABLE metrics_container_raw
    ADD COLUMN health text,
    ADD COLUMN stack  text;

INSERT INTO rules (name, subject, target, op, threshold, for_sec, severity) VALUES
    ('Container is unhealthy',  'container.unhealthy',  NULL, '=', 1, 120, 'warning'),
    ('The whole stack is down', 'stack.running_pct',    NULL, '=', 0, 900, 'warning');
