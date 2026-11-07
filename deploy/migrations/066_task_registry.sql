CREATE TABLE project_tasks (
    project_id integer NOT NULL REFERENCES profile_projects(id) ON DELETE CASCADE,
    number bigint NOT NULL CHECK (number > 0),
    title text NOT NULL,
    status text NOT NULL CHECK (status IN ('open', 'deferred', 'done', 'cancelled')),
    priority text NOT NULL DEFAULT '',
    position double precision,
    tags text[] NOT NULL DEFAULT '{}',
    blocked_by bigint[] NOT NULL DEFAULT '{}',
    external jsonb NOT NULL DEFAULT 'null',
    external_refs text[] NOT NULL DEFAULT '{}',
    external_url text NOT NULL DEFAULT '',
    created_at timestamptz,
    updated_at timestamptz,
    closed_at timestamptz,
    created_by text NOT NULL DEFAULT '',
    closed_by text NOT NULL DEFAULT '',
    touched_by text[] NOT NULL DEFAULT '{}',
    session text NOT NULL DEFAULT '',
    body jsonb NOT NULL DEFAULT 'null',
    markdown text NOT NULL DEFAULT '',
    source text NOT NULL,
    source_json jsonb NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (project_id, number)
);
CREATE INDEX project_tasks_status ON project_tasks(project_id, status, position, number);
CREATE INDEX project_tasks_tags ON project_tasks USING gin(tags);
CREATE INDEX project_tasks_external ON project_tasks USING gin(external_refs);

-- Source keys deduplicate facts recovered from a file. Panel events have no
-- source key and can carry their own kind, actor and payload without DDL.
CREATE TABLE task_events (
    id bigserial PRIMARY KEY,
    project_id integer NOT NULL,
    task_number bigint NOT NULL,
    kind text NOT NULL,
    at timestamptz NOT NULL,
    precision text NOT NULL CHECK (precision IN ('date', 'instant')),
    actor text NOT NULL DEFAULT '',
    origin text NOT NULL,
    source_key text,
    payload jsonb NOT NULL DEFAULT '{}',
    FOREIGN KEY (project_id, task_number) REFERENCES project_tasks(project_id, number) ON DELETE CASCADE,
    UNIQUE (project_id, task_number, source_key)
);
CREATE INDEX task_events_period ON task_events(at, project_id, task_number);

CREATE TABLE project_handoffs (
    project_id integer NOT NULL REFERENCES profile_projects(id) ON DELETE CASCADE,
    file text NOT NULL,
    key text NOT NULL DEFAULT '',
    session text NOT NULL DEFAULT '',
    kind text NOT NULL DEFAULT '',
    at timestamptz,
    date text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    cwd text NOT NULL DEFAULT '',
    git jsonb NOT NULL DEFAULT 'null',
    tasks jsonb NOT NULL DEFAULT 'null',
    task_numbers bigint[] NOT NULL DEFAULT '{}',
    body jsonb NOT NULL DEFAULT 'null',
    source text NOT NULL,
    source_json jsonb NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (project_id, file)
);
CREATE INDEX project_handoffs_tasks ON project_handoffs USING gin(task_numbers);

CREATE TABLE task_stores (
    project_id integer PRIMARY KEY REFERENCES profile_projects(id) ON DELETE CASCADE,
    root text NOT NULL,
    meta jsonb NOT NULL DEFAULT 'null'
);
