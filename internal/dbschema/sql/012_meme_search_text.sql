-- search_text holds free text derived from the tag-suggestion pass: the
-- on-image text the vision model transcribes, joined with the audio transcript
-- for videos. It is never shown to the user; it only feeds meme search.

ALTER TABLE memes ADD COLUMN IF NOT EXISTS search_text TEXT NOT NULL DEFAULT '';
