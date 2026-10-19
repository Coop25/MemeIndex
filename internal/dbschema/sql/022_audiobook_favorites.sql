-- audiobook_favorites is a per-user "favorited this book" marker, used to
-- power the Favorites filter on the audiobooks library.
CREATE TABLE IF NOT EXISTS audiobook_favorites (
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (audiobook_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_audiobook_favorites_user ON audiobook_favorites(user_id);
