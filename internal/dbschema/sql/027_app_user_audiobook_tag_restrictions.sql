-- app_user_audiobook_tag_restrictions locks an account to only the
-- audiobooks carrying at least one of these tags (e.g. a kids' account
-- restricted to a "kids" tag), reusing the same shared `tags` table
-- audiobook_tags and meme_tags already tag into. No rows for a user means
-- unrestricted - the default for every existing account.
CREATE TABLE IF NOT EXISTS app_user_audiobook_tag_restrictions (
    user_id TEXT NOT NULL,
    tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_app_user_audiobook_tag_restrictions_tag_id ON app_user_audiobook_tag_restrictions(tag_id);
