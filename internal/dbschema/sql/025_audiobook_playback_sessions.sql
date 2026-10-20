-- audiobook_playback_sessions tracks a lightweight "now playing" heartbeat per
-- (audiobook, user, device) so the player can tell the listener when the same
-- audiobook is actively playing on another of their devices right now, and let
-- them force that device to stop and pick up here from its last position.
CREATE TABLE IF NOT EXISTS audiobook_playback_sessions (
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_playing BOOLEAN NOT NULL DEFAULT FALSE,
    stop_requested BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (audiobook_id, user_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_audiobook_playback_sessions_lookup
    ON audiobook_playback_sessions(audiobook_id, user_id, is_playing, updated_at);
