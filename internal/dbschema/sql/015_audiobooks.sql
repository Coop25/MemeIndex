CREATE TABLE IF NOT EXISTS audiobooks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL,
    duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    storage_path TEXT NOT NULL,
    uploaded_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audiobooks_created_at ON audiobooks(created_at DESC);

-- audiobook_uploads tracks an in-progress chunked/resumable upload. A row is
-- created when the upload starts and deleted once it is finalized (moved into
-- audiobooks) or aborted. received_bytes advances only when a chunk lands at
-- exactly that offset, which is what makes the upload resumable after a
-- dropped connection or a page reload: the client re-reads received_bytes and
-- continues from there instead of restarting the whole file.
CREATE TABLE IF NOT EXISTS audiobook_uploads (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    total_bytes BIGINT NOT NULL,
    received_bytes BIGINT NOT NULL DEFAULT 0,
    temp_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audiobook_uploads_user ON audiobook_uploads(user_id);
CREATE INDEX IF NOT EXISTS idx_audiobook_uploads_updated_at ON audiobook_uploads(updated_at);

-- audiobook_progress is the per-user playback position for an audiobook. It is
-- what lets playback resume on a different machine: whichever device opens the
-- book next reads the latest row written by whichever device played it last.
CREATE TABLE IF NOT EXISTS audiobook_progress (
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (audiobook_id, user_id)
);
