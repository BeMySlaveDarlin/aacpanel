ALTER TABLE probes DROP CONSTRAINT probes_kind_check;
ALTER TABLE probes ADD CONSTRAINT probes_kind_check CHECK (kind IN ('tcp', 'http', 'status', 'wg'));

INSERT INTO probes (name, kind, target, interval_sec, timeout_sec, expect_status) VALUES
    ('API OpenAI', 'http', 'https://api.openai.com/v1/models', 60, 5, 401)
ON CONFLICT (name) DO NOTHING;

INSERT INTO probes (name, kind, target, interval_sec, timeout_sec) VALUES
    ('Anthropic status', 'status', 'https://status.claude.com/api/v2/status.json', 300, 5),
    ('OpenAI status',    'status', 'https://status.openai.com/api/v2/status.json', 300, 5)
ON CONFLICT (name) DO NOTHING;
