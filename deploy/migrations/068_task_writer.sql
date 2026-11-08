ALTER TABLE profile_projects ADD COLUMN task_writer text NOT NULL DEFAULT 'cctm'
    CHECK (task_writer IN ('cctm', 'panel'));
ALTER TABLE task_events ADD COLUMN actor_name text NOT NULL DEFAULT '';
ALTER TABLE project_handoffs ADD COLUMN session_name text NOT NULL DEFAULT '';
ALTER TABLE project_handoffs ADD COLUMN next_step text NOT NULL DEFAULT '';
CREATE INDEX project_handoffs_session ON project_handoffs(project_id, session, at);
