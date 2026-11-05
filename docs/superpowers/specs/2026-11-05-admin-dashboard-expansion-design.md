# Admin Dashboard Expansion — Design

Date: 2026-11-05
Branch: `admin-dashboard-expansion`

## Goal

The admin dashboard predates audiobooks. "Storage Used" counts only memes,
nothing reports how many audiobooks exist, and the admin workspace has no
tooling for audiobooks beyond MP3→M4B conversion. This work makes the
dashboard numbers accurate for the whole server, adds panels for audiobook and
operational state, and adds four admin pages: Storage, Audiobook Library, User
Activity, and Series & Tags.

Success means an admin can answer from the admin workspace alone: how much disk
is used and by what, how much is free, how many audiobooks there are and which
need attention, who is using the server, and when the last backup completed.

## Constraints

- Everything is admin-only: new endpoints live under `/api/admin/...` behind
  `permissionManageUsers`, matching existing admin routes.
- Audiobooks require PostgreSQL (`newAudiobookStore` returns nil without a
  `DatabaseURL`). Every audiobook-backed card, panel, or page hides or shows a
  "Requires PostgreSQL" state when `s.audiobooks == nil`; nothing errors.
- Production runs in Docker on Linux. Disk-space reads use `syscall.Statfs`
  behind a `//go:build linux` file; other platforms return "unknown" and the UI
  shows "Unknown".
- `static/app.js` is ~12.9k lines. New admin pages go in a new
  `static/admin-pages.js`, loaded with `defer` after `app.js` and reusing its
  globals (`escapeHTML`, `formatSize`, `formatRelativeTime`,
  `formatDateTimeZoned`, `ADMIN_TABS` rendering, `state.admin`). The service
  worker's precache list must include the new file. Dashboard changes stay in
  `app.js` next to `renderAdminDashboard`.
- New admin tabs register in `ADMIN_TABS` (single source of truth for the rail,
  header copy, and loading).
- Expensive aggregates reuse the existing admin read-cache pattern
  (`adminReadCacheTTL` memoization in `MemeManager`) or an equivalent
  server-side TTL cache in the client package.

## Phases

Each phase ships as its own PR off this branch line, in order. Phase 0 is a
bug fix and goes first because it affects data integrity and access control
today.

### Phase 0 — Fix tag merge destroying audiobook tags and restrictions

**Bug.** `PostgresStore.MergeTag` (`internal/accessor/postgres_store.go`)
re-points only `meme_tags` from the source tag to the target, then runs
`DELETE FROM tags WHERE id = sourceID`. `audiobook_tags.tag_id` and
`app_user_audiobook_tag_restrictions.tag_id` both reference `tags(id)` with
`ON DELETE CASCADE`, so merging a tag through Tag Hygiene:

1. silently strips that tag from every audiobook carrying it, and
2. deletes every account's tag restriction on it. An account restricted only to
   that tag (e.g. a kids' account restricted to `kids`) has no restriction rows
   left, which the schema defines as **unrestricted** — it can now see every
   audiobook.

The same happens on the early-return path where the source tag has no memes:
`DELETE FROM tags WHERE id = $1` runs even if audiobooks or restrictions use it.

**Fix.** Inside the same transaction, when the tables exist (reuse the
`to_regclass` check already used by `deleteOrphanTags`):

- Re-point `audiobook_tags` rows from source to target, skipping audiobooks
  that already carry the target, then delete leftover source rows.
- Re-point `app_user_audiobook_tag_restrictions` rows the same way per user.
- The "no memes use the source" early return must still re-point the audiobook
  tables, creating the target tag if needed, before deleting the source.
- `audiobooks` has no `updated_at` column, so there is nothing to bump.
- The returned count stays the number of memes affected (existing API);
  the response gains `audiobooks_affected` and `restrictions_moved` so the UI
  can report them.

**Tests.** Postgres-backed tests (pattern of existing accessor tests):
merge where the source is on memes + audiobooks; merge where the source is
on audiobooks only; merge where the target already exists on the book; merge
of a restriction tag — the user stays restricted to the target.

### Phase 1 — Accurate dashboard numbers

Backend: extend `adminDashboardResponse` in `internal/client/server.go`
(the manager's `AdminDashboardStats` stays meme-only).

New fields:

```go
AudiobookStats *adminAudiobookStats `json:"audiobook_stats,omitempty"` // nil without Postgres
Disk           *adminDiskStats      `json:"disk,omitempty"`            // nil when unknown
LastBackup     *adminLastBackup     `json:"last_backup,omitempty"`     // nil when none found
TotalStorageBytes int64             `json:"total_storage_bytes"`        // memes + audiobooks

type adminAudiobookStats struct {
    Count            int     `json:"count"`
    AddedLast30Days  int     `json:"added_last_30_days"`
    AddedPrev30Days  int     `json:"added_previous_30_days"`
    DurationSeconds  float64 `json:"duration_seconds"`
    MediaBytes       int64   `json:"media_bytes"`        // SUM(audiobooks.size_bytes)
    CoverBytes       int64   `json:"cover_bytes"`        // stat of cover files
    UploadTempBytes  int64   `json:"upload_temp_bytes"`  // SUM(audiobook_uploads.received_bytes)
    TotalBytes       int64   `json:"total_bytes"`
    BytesLast30Days  int64   `json:"bytes_last_30_days"`
    BytesPrev30Days  int64   `json:"bytes_previous_30_days"`
    Series           []adminAudiobookDayStat `json:"series"` // 30 days, cumulative count + bytes
}

type adminDiskStats struct {
    Path       string `json:"path"`        // config.DataDir
    TotalBytes uint64 `json:"total_bytes"`
    FreeBytes  uint64 `json:"free_bytes"`  // available to non-root (Bavail)
    UsedBytes  uint64 `json:"used_bytes"`
    Low        bool   `json:"low"`         // free < 10% of total OR free < 25 GiB
}

type adminLastBackup struct {
    CompletedAt time.Time `json:"completed_at"`
    SizeBytes   int64     `json:"size_bytes"`
    Stale       bool      `json:"stale"`    // older than 7 days
}
```

- Audiobook aggregates come from one new `audiobookStore` method,
  `AdminStats(ctx, now) (audiobookAdminStats, error)`, using SQL aggregates
  over `audiobooks` and `audiobook_uploads`. Cover bytes are summed by stat-ing
  `cover_path` values; results are cached with the admin TTL.
- Disk stats: new `internal/client/disk_usage_linux.go` /
  `disk_usage_other.go` exposing `diskUsage(path) (adminDiskStats, bool)`.
  The 25 GiB floor matches the backup part size so the warning means "the next
  backup part may not fit".
- Last backup: scan the backups directory via a new `portableBackup` method
  that returns the newest completed manifest's time and total size; current
  `exportStatus()` stays for the in-flight state.
- System health gains `Disk` (healthy unless `Low`) and `Last Backup`
  (healthy unless `Stale` or none) rows.

Frontend (`renderAdminDashboard` in `static/app.js`):

- **Storage Used** card: value = `total_storage_bytes`; note =
  "Memes X · Audiobooks Y"; sparkline = meme cumulative storage plus audiobook
  cumulative bytes per day.
- New **Audiobooks** card: count, note "N h total · +M in 30 days", trend from
  added 30d vs previous 30d, count sparkline. Hidden when `audiobook_stats` is
  absent.
- New **Disk** card: "X free of Y", a used/free meter, amber/red accent when
  `low`. Shows "Unknown" when `disk` is absent.
- Card grid grows from 6 to 8 cards; CSS grid must stay balanced at desktop
  and wrap cleanly at phone width.

### Phase 2 — New dashboard panels

Backend: one more field on the dashboard response,
`AudiobookActivity *adminAudiobookActivity`, plus `WorkQueues`.

```go
type adminAudiobookActivity struct {
    DailySeconds []adminDaySeconds      `json:"daily_seconds"` // 30 days
    TopBooks     []adminTopBook         `json:"top_books"`     // 5, by seconds in 30d
    TopListeners []adminTopListener     `json:"top_listeners"` // 5, by seconds in 30d
    Health       adminAudiobookHealth   `json:"health"`
}
type adminAudiobookHealth struct {
    AwaitingConversion int `json:"awaiting_conversion"`
    FailedConversion   int `json:"failed_conversion"`
    MissingCover       int `json:"missing_cover"`
    MissingChapters    int `json:"missing_chapters"`
    MissingSeries      int `json:"missing_series"`
}
type adminWorkQueues struct {
    DeleteQueue     int `json:"delete_queue"`
    SuggestedTags   int `json:"suggested_tags"`
    LinkRetries     int `json:"link_retries"`
    RejectedLinks   int `json:"rejected_links"`
    ConversionQueue int `json:"conversion_queue"`
}
```

- Listening data from `audiobook_listening_stats` (sums grouped by date, book,
  user). Listener names resolved via the user store.
- Conversion counts reuse the remux progress/status source used by
  `handleAudiobookRemuxStatus`; failed = remux failure markers present.
- Work queue counts are computed server-side here (pending-delete total,
  pending tag-suggestion memes, link retry queued/rejected lengths, remux
  backlog). Today `adminTabBadgeCounts()` derives badges client-side from the
  separate tab endpoints; after this, it prefers `work_queues` from the
  dashboard response when present and falls back to the existing sources, so
  badges and the strip always agree.

Frontend:

- **Listening activity** section: 30-day bar chart (reuse
  `buildAdminUploadChart` styling/tooltips), top books and top listeners lists.
- **Audiobook health** section: five counts, each a button that opens the
  Audiobook Library tab with the matching filter.
- **Work queues** strip above the analytics row: one chip per queue with its
  count, linking to its tab; zero counts render muted.
- Audiobook sections hidden without Postgres.

### Phase 3 — Storage page

Tab: `storage`, group "Operations", label "Storage", panel
`admin-storage-panel`.

Endpoints:

- `GET /api/admin/storage` → breakdown + largest files + in-progress uploads.
  Cached 5 minutes server-side; `?refresh=1` bypasses the cache.
- `POST /api/admin/storage/orphans/scan` → runs the orphan scan, returns
  results (also cached until the next scan).
- `POST /api/admin/storage/orphans/delete` with `{ "paths": [...] }` → deletes
  only paths present in the latest scan result; returns per-path outcome.
- `DELETE /api/admin/audiobooks/uploads/{id}` → discard an in-progress upload,
  using the same lock + remove + `DiscardUpload` sequence as the sweeper
  (extract that body into a shared helper).

Breakdown: on-disk bytes per directory under `DataDir` — `uploads`,
`thumbnails`, `audiobooks` (excluding `covers`, `tmp`), `audiobooks/covers`,
`audiobooks/tmp`, `downloads`, `backups` — plus "other" for anything else at
the top level, and the disk total/free from Phase 1. Measured with
`filepath.WalkDir`, never following symlinks.

Largest files: top 25 across meme media and audiobook media by DB size, each
with type, owner/uploader, and a link that opens the meme modal or audiobook
edit modal.

Orphans:

- *Files without rows*: files in `uploads`, `thumbnails`, `audiobooks`,
  `audiobooks/covers` not referenced by any meme file/preview path, audiobook
  `storage_path`, or `cover_path`. Files in `audiobooks/tmp` not referenced by
  an `audiobook_uploads.temp_path`. Remux failure markers and files touched in
  the last hour are excluded (they may belong to an in-flight write).
- *Rows without files*: memes and audiobooks whose media path does not exist.
  These are reported only; there is no delete action for rows in this page.
- `backups/` and `downloads/` are never scanned for deletion.
- Deletion requires a confirm dialog listing count and total bytes, re-checks
  each path is still inside its allowed directory and still unreferenced, and
  writes one audit-log entry per batch (actor, count, bytes).

In-progress uploads: table of `audiobook_uploads` — title, uploader, received /
total bytes, last updated, Discard button (confirm). Note that the sweeper
removes them after 24h of inactivity.

### Phase 4 — Audiobook Library page

Tab: `audiobook-library`, group new "Audiobooks" (move `audiobook-conversion`
into it), label "Library".

Endpoint: `GET /api/admin/audiobooks?sort=&dir=&filter=&page=&page_size=`.

Columns: title, author, format (MP3/M4B from mime/extension), size, duration,
uploader, uploaded, cover ✓, chapters ✓, series ✓, active listeners (distinct
users with progress in 30d), total hours listened.

Sort: any column. Filters: `no-cover`, `no-chapters`, `no-series`,
`not-converted`, `conversion-failed` (the same set the Phase 2 health panel
links to; the dashboard passes `filter` when opening the tab). Paging reuses
the admin table pager (`admin-page-prev/next`). Row click opens the existing
audiobook edit modal — no new editing UI.

### Phase 5 — User Activity page

Tab: `user-activity`, group "Overview", label "User Activity",
sibling `users`.

Endpoint: `GET /api/admin/user-activity`.

One row per user: display name, memes uploaded, audiobooks uploaded,
favorites, listening hours (30d and all-time), books finished
(`audiobook_progress.finished_at` not null), last active. Sortable client-side
(user counts are small). Row click opens Audit Logs filtered to that actor.
`/api/admin/audit-logs` has no actor filter today, so this phase adds an
`actor` query parameter (user id) to it and an actor chip to the Audit Logs
tab that clears the filter. Read-only; permissions remain on
the Users tab.

### Phase 6 — Series & audiobook tags page

Tab: `audiobook-series`, group "Audiobooks", label "Series & Tags".

Series (existing `PATCH`/`DELETE /api/audiobook-series/{id}` cover rename and
delete; add what is missing):

- List all series with book count and books in sequence order.
- Rename (existing PATCH).
- Merge A into B: `POST /api/admin/audiobook-series/{id}/merge` with
  `{ "into": "<id>" }` — moves books (keeping their sequence numbers), deletes
  A, in one transaction.
- Delete: allowed only when the series has no books (existing DELETE, with a
  server-side empty check added if absent).
- Reorder: edit sequence numbers inline; saved via the existing per-book
  update endpoint.

Tags: list tags used by audiobooks with audiobook count (and meme count, since
the table is shared), and which accounts are restricted to each tag. Merging
uses the existing Tag Hygiene merge, which Phase 0 makes audiobook-safe; this
page links to it rather than adding a second merge tool.

## Error handling

- Every new dashboard sub-query is independent: a failure logs, leaves that
  field nil/zero, and marks the related System health row unhealthy, matching
  how the handler treats shares and activity today. The dashboard never 500s
  because one aggregate failed.
- Pages show the standard `status-text` error line on fetch failure and keep
  any previously loaded data.
- Destructive actions (orphan delete, upload discard, series merge/delete)
  confirm first, report per-item results, and write audit entries.

## Testing

- Go unit tests: audiobook admin aggregates, disk-low thresholds (pure
  function over total/free), last-backup discovery, orphan detection
  (temp dir fixture with referenced, unreferenced, recent, and symlinked files),
  path-containment checks on orphan delete, user-activity rollup, series merge,
  and the Phase 0 tag merge cases.
- Handler tests: every new endpoint rejects non-admins (pattern of
  `audiobook_permissions_test.go`), and returns the no-Postgres state cleanly.
- Frontend: extend `scripts/admin-dashboard.test.cjs` for the new cards
  (storage split text, audiobook card hidden without stats, disk Unknown/low);
  add `scripts/admin-pages.test.cjs` for library filtering/sorting and the
  storage page rendering.
- Each phase: `go test ./...` and the `.cjs` tests pass before its PR.

## Out of scope

- Editing user permissions from the User Activity page.
- Automatic orphan deletion or scheduled scans.
- Deleting database rows whose media file is missing.
- Changing the 24h stale-upload sweeper.
