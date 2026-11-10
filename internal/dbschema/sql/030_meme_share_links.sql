-- One row per share link ever issued. A link serves the file only while its
-- generation is the meme's current share_generation and the share has not
-- expired; older rows are kept so a dead link can still redirect to the meme.
CREATE TABLE IF NOT EXISTS meme_share_links (
    slug TEXT PRIMARY KEY,
    meme_id TEXT NOT NULL REFERENCES memes(id) ON DELETE CASCADE,
    generation BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (meme_id, generation)
);
