ALTER TABLE probes ADD COLUMN components text;

ALTER TABLE probes ADD CONSTRAINT probes_components_status
    CHECK (components IS NULL OR kind = 'status');

ALTER TABLE probes ADD CONSTRAINT probes_components_not_blank
    CHECK (components IS NULL OR btrim(components) <> '');

ALTER TABLE probe_results DROP CONSTRAINT probe_results_outcome_check;
ALTER TABLE probe_results ADD CONSTRAINT probe_results_outcome_check
    CHECK (outcome IN ('ok', 'network', 'timeout', 'status', 'degraded', 'config'));

UPDATE probes
   SET target = 'https://status.claude.com/api/v2/summary.json',
       components = 'api.anthropic.com, Claude Code'
 WHERE name = 'Anthropic status' AND kind = 'status';

UPDATE probes
   SET target = 'https://status.openai.com/api/v2/summary.json',
       components = 'Chat Completions, Responses, Codex API'
 WHERE name = 'OpenAI status' AND kind = 'status';
