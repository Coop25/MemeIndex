-- Indexes that back the SQL-side meme browse/search query. None of these depend
-- on a contrib extension, so they are safe to apply unconditionally.
-- (memes.created_at already has idx_memes_created_at from 001; meme_tags(meme_id)
-- is covered by that table's primary key.)

CREATE INDEX IF NOT EXISTS idx_memes_updated_at ON memes(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memes_content_type ON memes(content_type);
CREATE INDEX IF NOT EXISTS idx_memes_size_bytes ON memes(size_bytes DESC);
CREATE INDEX IF NOT EXISTS idx_memes_lower_original_name ON memes(LOWER(original_name));
