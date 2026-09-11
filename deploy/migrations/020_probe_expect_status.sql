ALTER TABLE probes ADD COLUMN expect_status integer
    CHECK (expect_status IS NULL OR expect_status BETWEEN 100 AND 599);

ALTER TABLE probes ADD CONSTRAINT probes_expect_status_http
    CHECK (expect_status IS NULL OR kind = 'http');

UPDATE probes SET kind = 'http', target = 'https://api.anthropic.com/v1/models', expect_status = 401
    WHERE name = 'API Anthropic' AND kind = 'tcp';
UPDATE probes SET kind = 'http', target = 'https://www.googleapis.com/generate_204', expect_status = 204
    WHERE name = 'API Google' AND kind = 'tcp';
UPDATE probes SET kind = 'http', target = 'https://api.telegram.org/bot/getMe', expect_status = 404
    WHERE name = 'API Telegram' AND kind = 'tcp';
