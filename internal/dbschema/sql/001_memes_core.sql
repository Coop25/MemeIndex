CREATE TABLE IF NOT EXISTS memes (
    id TEXT PRIMARY KEY,
    original_name TEXT NOT NULL,
    stored_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    content_type TEXT NOT NULL,
    content_hash TEXT,
    size_bytes BIGINT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    hidden_from_app BOOLEAN NOT NULL DEFAULT FALSE,
    pending_delete BOOLEAN NOT NULL DEFAULT FALSE,
    delete_requested_by_user_id TEXT NOT NULL DEFAULT '',
    delete_requested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS tags (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS meme_tags (
    meme_id TEXT NOT NULL REFERENCES memes(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (meme_id, tag_id)
);

CREATE TABLE IF NOT EXISTS user_favorites (
    user_id TEXT NOT NULL,
    meme_id TEXT NOT NULL REFERENCES memes(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, meme_id)
);

CREATE TABLE IF NOT EXISTS reel_sessions (
    id TEXT PRIMARY KEY,
    history JSONB NOT NULL,
    position INTEGER NOT NULL,
    last_activity TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS meme_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    meme_id TEXT NOT NULL,
    action TEXT NOT NULL,
    actor_user_id TEXT NOT NULL DEFAULT '',
    actor_username TEXT NOT NULL DEFAULT '',
    actor_display_name TEXT NOT NULL DEFAULT '',
    actor_avatar_url TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name);
CREATE INDEX IF NOT EXISTS idx_meme_tags_tag_id ON meme_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_user_favorites_user_id ON user_favorites(user_id);
CREATE INDEX IF NOT EXISTS idx_memes_created_at ON memes(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reel_sessions_last_activity ON reel_sessions(last_activity);
CREATE INDEX IF NOT EXISTS idx_meme_audit_logs_meme_id_created_at ON meme_audit_logs(meme_id, created_at DESC);
