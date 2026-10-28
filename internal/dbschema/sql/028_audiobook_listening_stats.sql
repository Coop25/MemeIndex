-- audiobook_listening_stats accumulates how many seconds a user has actually
-- spent listening to a given audiobook, bucketed by day. It is derived from
-- playback heartbeats (see HeartbeatPlaybackSession) rather than stored as a
-- raw per-heartbeat event log, so a stats view can show daily/weekly totals
-- and a listening streak with one small, bounded table.
CREATE TABLE IF NOT EXISTS audiobook_listening_stats (
    user_id TEXT NOT NULL,
    audiobook_id TEXT NOT NULL REFERENCES audiobooks(id) ON DELETE CASCADE,
    listen_date DATE NOT NULL,
    seconds_listened DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, audiobook_id, listen_date)
);

CREATE INDEX IF NOT EXISTS idx_audiobook_listening_stats_user_date
    ON audiobook_listening_stats(user_id, listen_date DESC);

-- finished_at records the first time this user's saved position crossed the
-- "finished" threshold for the book, so a finished-books history can be
-- listed in the order books were actually finished. Unlike updated_at, it
-- isn't disturbed by relistening afterwards.
ALTER TABLE audiobook_progress ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
