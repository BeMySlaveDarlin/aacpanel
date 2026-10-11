The JSONL samples retain the response fields exercised by these tests from
Claude 2.1.282 protocol captures: `list_models.json` and `local_command.jsonl`.
The model list is cut to its first item; the command sample keeps its text and
result, without account data, environment, usage counters or session identity.
The letter samples come from a Claude 2.1.296 contract run. They keep only the
letter-related stdout and transcript records, without paths, environment,
usage counters or session identity; command IDs are replaced with neutral
names. Both letters are processed, but the model refuses to obey commands
from a peer. Tests pair those refusals with synthetic user recall answers and
substitute a factual payload into the recorded transcript shapes. The busy
letter is a `queued_command` attachment; the idle letter is a `user` record.
Neither retains the stdin
origin, and neither stdout replay echoes it. The `rendered` attachment text
is kept so the report can show its framing without making it a verdict.
The background sample keeps the task list, successful first turn and natural
completion that empties the list. The sleep refusal comes from another run
of the same version: a leading `sleep 40` chained to `echo woke` is blocked.
Permission and task cases use the shapes exercised by the holder tests in
`internal/stream`. Verdict tests also exercise deliberately broken variants.
