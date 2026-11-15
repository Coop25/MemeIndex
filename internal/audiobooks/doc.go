// Package audiobooks persists audiobooks in Postgres: uploads (resumable,
// tracked by byte range), finished books and their files, chapters, cover
// art, series, tags, bookmarks, playback progress and listening stats, plus
// the aggregate queries behind the admin dashboard. HTTP handling, sessions
// and permissions stay in internal/client, which talks to this package
// through the Store interface.
package audiobooks
