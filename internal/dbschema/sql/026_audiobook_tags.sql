-- audiobook_tags links audiobooks to the same `tags` table memes already use,
-- so a tag like "sci-fi" is one shared vocabulary across both libraries
-- instead of a second, parallel tag system.
CREATE TABLE IF NOT EXISTS audiobook_tags (
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (audiobook_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_audiobook_tags_tag_id ON audiobook_tags(tag_id);
