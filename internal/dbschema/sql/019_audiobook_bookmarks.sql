CREATE TABLE IF NOT EXISTS audiobook_bookmarks (
    id TEXT PRIMARY KEY,
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    position_seconds DOUBLE PRECISION NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audiobook_bookmarks_lookup ON audiobook_bookmarks(audiobook_id, user_id, position_seconds);
