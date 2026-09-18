-- The branch a review of this project is measured against. An empty string is
-- not a choice: it means nobody picked one, and the screen offers main as the
-- default without writing it down as an answer.
ALTER TABLE profile_projects ADD COLUMN base_branch text NOT NULL DEFAULT '';

-- What git refuses to take as a ref name, refused here as well: a leading dash
-- reads as a flag, a range is two names rather than one, @{ opens a reflog
-- address, and .lock is the name of a file git keeps for itself.
ALTER TABLE profile_projects ADD CONSTRAINT profile_projects_base_branch_check CHECK (
    base_branch = ''
    OR (
        length(base_branch) <= 200
        AND base_branch !~ '^-'
        AND base_branch !~ '\.\.'
        AND base_branch !~ '@\{'
        AND base_branch !~ '\.lock$'
        AND base_branch !~ '[[:cntrl:][:space:]~^:?*\[\\]'
    )
);
