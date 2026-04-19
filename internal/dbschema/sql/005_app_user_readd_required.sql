CREATE TABLE IF NOT EXISTS app_user_readd_required (
    user_id TEXT PRIMARY KEY,
    blocked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
