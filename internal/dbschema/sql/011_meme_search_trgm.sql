-- Trigram indexes that make the substring (LIKE '%term%') meme search
-- index-accelerated instead of a sequential scan.
--
-- This migration is applied best-effort: it needs the bundled pg_trgm contrib
-- extension, which a locked-down or managed database role may not be permitted
-- to CREATE. If that happens the app still runs and search still works, just
-- without the index speed-up, so ApplyOptional swallows the failure.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_memes_original_name_trgm
    ON memes USING gin (LOWER(original_name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_memes_notes_trgm
    ON memes USING gin (LOWER(notes) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_memes_source_url_trgm
    ON memes USING gin (LOWER(source_url) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_tags_name_trgm
    ON tags USING gin (name gin_trgm_ops);
