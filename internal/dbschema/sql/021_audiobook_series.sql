-- audiobook_series is a named grouping (e.g. "Bobiverse") that an audiobook
-- can optionally belong to, with a sequence number for ordering within it
-- (e.g. "Book 1", "Book 2").
CREATE TABLE IF NOT EXISTS audiobook_series (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_audiobook_series_name ON audiobook_series(lower(name));

ALTER TABLE audiobooks ADD COLUMN IF NOT EXISTS series_id TEXT REFERENCES audiobook_series(id) ON DELETE SET NULL;
ALTER TABLE audiobooks ADD COLUMN IF NOT EXISTS series_sequence DOUBLE PRECISION;

CREATE INDEX IF NOT EXISTS idx_audiobooks_series ON audiobooks(series_id, series_sequence);
