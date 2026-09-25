-- The main checkout of the git worktree a session ran in, empty anywhere else.
-- A worktree lies beside its repository and on no map, and it is often gone by
-- the time the usage is looked at: the agent notes the repository while the
-- disk can still tell, and the session is placed by it.
ALTER TABLE usage_sessions ADD COLUMN checkout text NOT NULL DEFAULT '';
