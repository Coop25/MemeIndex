ALTER TABLE memes ADD COLUMN IF NOT EXISTS content_hash TEXT;
ALTER TABLE memes ADD COLUMN IF NOT EXISTS hidden_from_app BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE memes ADD COLUMN IF NOT EXISTS pending_delete BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE memes ADD COLUMN IF NOT EXISTS delete_requested_by_user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE memes ADD COLUMN IF NOT EXISTS delete_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_memes_content_hash ON memes(content_hash);
CREATE INDEX IF NOT EXISTS idx_memes_hidden_from_app ON memes(hidden_from_app);
CREATE INDEX IF NOT EXISTS idx_memes_pending_delete ON memes(pending_delete);
