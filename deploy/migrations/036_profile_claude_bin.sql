ALTER TABLE profiles
    ADD COLUMN IF NOT EXISTS claude_bin text NOT NULL DEFAULT ''
    CHECK (claude_bin = '' OR claude_bin LIKE '/%');
