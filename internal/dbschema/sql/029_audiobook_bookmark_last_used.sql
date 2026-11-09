-- last_used_at drives bookmark expiry: a bookmark is "used" when it is created
-- or jumped to, and one left unused past the configured window is deleted.
-- Existing bookmarks start their clock at the time this runs rather than at
-- created_at, so turning expiry on doesn't wipe out every old bookmark the
-- moment the server restarts.
ALTER TABLE audiobook_bookmarks ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_audiobook_bookmarks_last_used ON audiobook_bookmarks(last_used_at);
