-- received_ranges tracks exactly which byte ranges of an in-progress upload
-- have landed on disk, as a JSON array of half-open [start, end) pairs, e.g.
-- [[0,8388608],[16777216,25165824]]. This replaces a single running offset
-- now that chunks can arrive out of order from several concurrent streams:
-- received_bytes is kept as the sum of the (merged, non-overlapping) ranges
-- so existing progress/completion reads don't need to change.
ALTER TABLE audiobook_uploads ADD COLUMN IF NOT EXISTS received_ranges JSONB NOT NULL DEFAULT '[]';
