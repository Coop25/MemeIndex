ALTER TABLE app_users ADD COLUMN IF NOT EXISTS session_version BIGINT NOT NULL DEFAULT 1;

-- Preserve the last-issued session version across a delete so that re-adding the
-- user starts from a strictly higher version and any token minted before the
-- delete stops validating.
ALTER TABLE app_user_readd_required ADD COLUMN IF NOT EXISTS session_version BIGINT NOT NULL DEFAULT 1;
