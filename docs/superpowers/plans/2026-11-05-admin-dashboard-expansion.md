# Admin Dashboard Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the admin dashboard accurate for memes + audiobooks, add audiobook/operational panels, and add Storage, Audiobook Library, User Activity, and Series & Tags admin pages — after first fixing the tag-merge cascade bug.

**Architecture:** Go backend: new aggregates live in `internal/client` (where the audiobook store is) and extend `adminDashboardResponse`; the meme-only `manager.AdminDashboardStats` is untouched except where Phase 0 changes the accessor. Frontend: dashboard work stays in `static/app.js` next to `renderAdminDashboard`, with pure HTML-builder functions placed *before* `renderAdminDashboard` so `scripts/*.test.cjs` can slice and run them; new pages live in a new `static/admin-pages.js`.

**Tech Stack:** Go (net/http, pgx/pgxpool), PostgreSQL, vanilla JS, `node --test` with `node:vm`.

**Spec:** `docs/superpowers/specs/2026-11-05-admin-dashboard-expansion-design.md`

## Global Constraints

- Every new endpoint is under `/api/admin/...`, registered in `Server.Routes()` (`internal/client/server.go`) with `s.withAPIAuth(..., permissionManageUsers)`, and added to the endpoint list in `TestActualRoutesDenyViewerAdministrativeActions` (`internal/client/deep_security_test.go`) so a viewer gets 403.
- When `s.audiobooks == nil` (no `DatabaseURL`): audiobook dashboard fields are omitted (`omitempty` nil pointers); audiobook admin endpoints return `200 {"state":"unavailable"}`; UI hides audiobook cards/panels and shows "Requires PostgreSQL" on audiobook pages.
- Disk stats: `syscall.Statfs` only in a `//go:build linux` file; other platforms return `ok=false`; UI shows "Unknown".
- Disk low = free < 10% of total **or** free < 25 GiB (`25 << 30`). Backup stale = newest completed backup older than 7 days.
- Storage page cache TTL: 5 minutes; `?refresh=1` bypasses.
- Orphan scan never touches `backups/` or `downloads/`; skips files modified in the last hour and remux failure markers (`audiobookRemuxFailurePath`); never follows symlinks.
- Destructive admin actions confirm in the UI first and return per-item outcomes.
- New tab ids: `storage` (group Operations), `audiobook-library` and `audiobook-series` (new group "Audiobooks"; move `audiobook-conversion` into it), `user-activity` (group Overview, sibling `users`).
- Each phase is its own PR. Before each PR: `go test ./...` and `node --test scripts/` pass.
- Postgres-backed tests skip unless `MEMEINDEX_TEST_DATABASE_URL` is set; run them with it set (dev DB from `docker-compose.dev.yml`) before each PR.
- `static/admin-pages.js` is loaded with `<script src="/static/admin-pages.js" defer></script>` placed after the `app.js` tag in `static/index.html`. No service-worker change.

## Review Focus

1. **Merging a tag that is both a meme tag and a user's only audiobook restriction** — the user must stay restricted (to the target). Pinned in Task 1.
2. **Orphan delete with a path that escapes its directory** (`../`, absolute path, symlink, or a path not in the last scan) — must be refused per item, nothing outside `DataDir` removed. Pinned in Task 9.
3. **A file being written while the scan runs** (in-flight upload temp file, fresh remux output) — must not be reported as an orphan. Pinned in Task 8 (recent-mtime and `audiobook_uploads.temp_path` exclusion).
4. **Server without Postgres** — dashboard still renders, no audiobook cards, no 500s from new endpoints. Pinned in Tasks 3, 4, 12, 14.
5. **Empty library** (0 audiobooks, 0 listening rows, no backups) — cards show 0 / "No backups yet", charts render empty without NaN. Pinned in Tasks 5 and 7.

---

## Phase 0 — Tag merge keeps audiobook tags and restrictions

### Task 1: Re-point audiobook tags and restrictions in `MergeTag`

**Files:**
- Modify: `internal/accessor/postgres_store.go` (`MergeTag`, ~line 656)
- Modify: `internal/manager/meme_manager.go` (`TagMergeResult`, `MergeTags` ~line 1434)
- Modify: `internal/accessor/store.go` (store interface signature for `MergeTag`, and the JSON file store implementation in `internal/accessor/meme_store.go` — it has no audiobook tables, return zeros)
- Test: `internal/accessor/postgres_store_tag_merge_test.go` (new)

**Interfaces:**
- Produces: `func (s *PostgresStore) MergeTag(sourceTag, targetTag string, actor AuditActor) (TagMergeCounts, error)` with
  `type TagMergeCounts struct { Memes, Audiobooks, Restrictions int }` in `internal/accessor`.
  `manager.TagMergeResult` gains `AudiobooksAffected int \`json:"audiobooks_affected"\`` and `RestrictionsMoved int \`json:"restrictions_moved"\``.

- [ ] **Step 1: Write failing tests** in `postgres_store_tag_merge_test.go`, skipping without `MEMEINDEX_TEST_DATABASE_URL`. The test helper creates `audiobooks`, `audiobook_tags`, `app_user_audiobook_tag_restrictions` with `dbschema.Apply` for `015_audiobooks.sql`, `026_audiobook_tags.sql`, `027_app_user_audiobook_tag_restrictions.sql` after `NewPostgresStore`.
  - `TestMergeTagMovesAudiobookTags`: meme M and audiobook B both tagged `kidz`; merge `kidz`→`kids`. Assert `counts == {Memes:1, Audiobooks:1, Restrictions:0}`, B's tags == `["kids"]`, tag `kidz` no longer exists.
  - `TestMergeTagAudiobookOnlySource`: only audiobook B tagged `kidz` (no memes). Merge. Assert B tagged `kids`, `counts.Audiobooks == 1`.
  - `TestMergeTagTargetAlreadyOnAudiobook`: B tagged `kidz` and `kids`. Merge. Assert B has exactly one row for `kids`, no error.
  - `TestMergeTagKeepsUserRestricted`: user `u1` restricted to `kidz` only; merge. Assert `u1`'s restriction rows == `[kids]` (non-empty), `counts.Restrictions == 1`.
- [ ] **Step 2: Run** `MEMEINDEX_TEST_DATABASE_URL=... go test ./internal/accessor/ -run TestMergeTag -v` — expect FAIL (audiobook tag gone / restriction rows empty).
- [ ] **Step 3: Implement.** In the transaction, before any `DELETE FROM tags`: for each of `audiobook_tags` (key column `audiobook_id`) and `app_user_audiobook_tag_restrictions` (key column `user_id`) that exists (same `to_regclass` check as `deleteOrphanTags` — extract it as `func tableExists(ctx, db queryable, name string) (bool, error)`), upsert the target tag id, then `UPDATE ... SET tag_id=$target WHERE tag_id=$source AND NOT EXISTS (same key with target)`, then `DELETE ... WHERE tag_id=$source`; count re-pointed + deduped keys. The "no memes" early return must run this too (creating the target tag only if an audiobook table had rows). Thread the counts through `MergeTags` into `TagMergeResult`.
- [ ] **Step 4: Run** the Step 2 command plus `go test ./...` — expect PASS.
- [ ] **Step 5: Surface counts in the Tag Hygiene merge status message** in `static/app.js` (the handler for `#admin-tag-merge-form`): append " and N audiobooks" when `audiobooks_affected > 0`, and " (N account restrictions moved)" when `restrictions_moved > 0`.
- [ ] **Step 6: Commit** `Fix tag merge dropping audiobook tags and account restrictions`.

---

## Phase 1 — Accurate dashboard numbers

### Task 2: Disk usage probe

**Files:**
- Create: `internal/client/disk_usage.go` (types + pure threshold logic), `internal/client/disk_usage_linux.go`, `internal/client/disk_usage_other.go`
- Test: `internal/client/disk_usage_test.go`

**Interfaces:**
- Produces:
  `type adminDiskStats struct { Path string \`json:"path"\`; TotalBytes, FreeBytes, UsedBytes uint64 (json total_bytes/free_bytes/used_bytes); Low bool \`json:"low"\` }`
  `func diskUsage(path string) (adminDiskStats, bool)` (platform files; linux uses `Statfs`, `FreeBytes = Bavail*Bsize`, `TotalBytes = Blocks*Bsize`, `UsedBytes = Total - Bfree*Bsize`)
  `func diskIsLow(total, free uint64) bool` and `const diskLowFloorBytes = 25 << 30`

- [ ] **Step 1: Failing test** `TestDiskIsLow`: table — `(1000 GiB, 50 GiB)`→true (5%), `(1000 GiB, 150 GiB)`→false, `(100 GiB, 20 GiB)`→true (under 25 GiB floor although 20%), `(0, 0)`→false.
- [ ] **Step 2: Run** `go test ./internal/client/ -run TestDiskIsLow -v` — FAIL (undefined).
- [ ] **Step 3: Implement** the three files.
- [ ] **Step 4: Run** — PASS. Also `GOOS=linux go vet ./internal/client/` and `GOOS=windows go vet ./internal/client/` both clean.
- [ ] **Step 5: Commit** `Add data-volume disk usage probe`.

### Task 3: Audiobook admin aggregates

**Files:**
- Create: `internal/client/admin_audiobook_stats.go`
- Modify: `internal/client/audiobook_store.go` (add method to `audiobookStore` interface + `postgresAudiobookStore`; any test fakes implementing the interface get a stub)
- Test: `internal/client/admin_audiobook_stats_test.go`

**Interfaces:**
- Produces:
  `type adminAudiobookDayStat struct { Date string \`json:"date"\`; Count int \`json:"count"\`; Bytes int64 \`json:"bytes"\` }` — **cumulative** count/bytes as of end of that day.
  `type adminAudiobookStats struct` with exactly the fields in the spec Phase 1 block (`count, added_last_30_days, added_previous_30_days, duration_seconds, media_bytes, cover_bytes, upload_temp_bytes, total_bytes, bytes_last_30_days, bytes_previous_30_days, series`).
  `audiobookStore.AdminStats(ctx context.Context, now time.Time) (adminAudiobookStats, error)`; `TotalBytes = MediaBytes + CoverBytes + UploadTempBytes`; `Series` has 30 entries, dates `YYYY-MM-DD` UTC, last = `now`'s date (same window as `MetricSeries`).
  `func (s *Server) cachedAudiobookAdminStats(ctx context.Context) (*adminAudiobookStats, error)` — nil,nil when `s.audiobooks == nil`; memoized for `manager`'s `adminReadCacheTTL` (export it as `manager.AdminReadCacheTTL` if unexported) behind a mutex on `Server`.

- [ ] **Step 1: Failing test** `TestAudiobookAdminStats` (Postgres, use `newTestAudiobookStore`): insert 3 books — sizes 100, 200, 300; created 2d, 40d, 70d ago; durations 3600 each; one has a cover file of 10 bytes written under the store's covers dir; one `audiobook_uploads` row with `received_bytes=50`. Assert `Count=3, AddedLast30Days=1, AddedPrev30Days=1, DurationSeconds=10800, MediaBytes=600, CoverBytes=10, UploadTempBytes=50, TotalBytes=660, BytesLast30Days=100, BytesPrev30Days=200, len(Series)=30, Series[29].Count=3, Series[0].Count=2`.
- [ ] **Step 2: Failing test** `TestAudiobookAdminStatsEmpty`: fresh store with no books → all zeros, `len(Series)=30`, no error.
- [ ] **Step 3: Run** `go test ./internal/client/ -run TestAudiobookAdminStats -v` — FAIL.
- [ ] **Step 4: Implement** — one SQL aggregate query over `audiobooks` (counts/sums with `FILTER (WHERE created_at >= ...)`), one `SUM(received_bytes)` over `audiobook_uploads`, `os.Stat` over non-empty `cover_path`s (missing files count 0), daily cumulative series built in Go like `computeAdminDashboard` does.
- [ ] **Step 5: Run** — PASS. **Commit** `Add audiobook admin storage and count aggregates`.

### Task 4: Last backup discovery + dashboard response fields

**Files:**
- Modify: `internal/client/portable_backup.go` (new method)
- Modify: `internal/client/server.go` (`adminDashboardResponse`, `handleAdminDashboard`)
- Test: `internal/client/portable_backup_last_test.go`, `internal/client/admin_dashboard_response_test.go`

**Interfaces:**
- Consumes: `diskUsage`, `cachedAudiobookAdminStats` (Tasks 2–3).
- Produces:
  `type adminLastBackup struct { CompletedAt time.Time \`json:"completed_at"\`; SizeBytes int64 \`json:"size_bytes"\`; Stale bool \`json:"stale"\` }`
  `func (b *portableBackup) lastCompletedBackup(now time.Time) (*adminLastBackup, error)` — newest backup dir under `backupDir()` that has a manifest; `CompletedAt` = manifest `CreatedAt`; `SizeBytes` = sum of part file sizes; `Stale` = `now.Sub(CompletedAt) > 7*24h`; nil when none.
  `adminDashboardResponse` gains `AudiobookStats *adminAudiobookStats \`json:"audiobook_stats,omitempty"\``, `Disk *adminDiskStats \`json:"disk,omitempty"\``, `LastBackup *adminLastBackup \`json:"last_backup,omitempty"\``, `TotalStorageBytes int64 \`json:"total_storage_bytes"\``.
  System health gains rows `{Name:"Disk"}` (Status "X free" / "Low: X free" / "Unknown"; Healthy = ok && !Low) and `{Name:"Last Backup"}` (Status relative age or "None yet"; Healthy = exists && !Stale).

- [ ] **Step 1: Failing test** `TestLastCompletedBackup`: temp data dir with two backup dirs (manifest `CreatedAt` 10d and 2d ago, part files 5 and 7 bytes) and one dir without a manifest (newer mtime). Assert returns the 2d one, `SizeBytes=7`, `Stale=false`; with `now` 9 days later `Stale=true`; empty dir → nil, nil. Read the existing manifest/part filename conventions in `portable_backup.go` (`listBackups`-style code near line 181) and write fixtures that match them.
- [ ] **Step 2: Failing test** `TestAdminDashboardWithoutPostgres`: server from `newShareTestServer` (no audiobook store), GET `/api/admin/dashboard` as admin → 200, JSON has no `audiobook_stats` key, `total_storage_bytes == total_size_bytes`, health contains names `Disk` and `Last Backup`.
- [ ] **Step 3: Run** both — FAIL.
- [ ] **Step 4: Implement** `lastCompletedBackup` and the handler additions; each sub-query failure logs and leaves its field nil (never 500).
- [ ] **Step 5: Run** — PASS. **Commit** `Report total storage, disk space, and last backup on the dashboard`.

### Task 5: Dashboard cards (frontend)

**Files:**
- Modify: `static/app.js` (`renderAdminDashboard` ~line 2983; new pure builder above it), `static/styles.css` (`.admin-dashboard-grid` for 8 cards; `.admin-disk-meter`)
- Test: `scripts/admin-dashboard.test.cjs`

**Interfaces:**
- Produces: `function buildAdminDashboardMetrics(dashboard) -> Array<metric>` (the array currently inline in `renderAdminDashboard`, moved out and extended), defined between `adminChartTooltipAttributes` and `renderAdminDashboard` so the existing slice in the test file loads it. `formatSize` must also be loaded into the test context (slice it the same way as `escapeHTML`).
  Storage metric: `value = formatSize(total_storage_bytes)`, `note = "Memes ${formatSize(total_size_bytes)} · Audiobooks ${formatSize(audiobook_stats.total_bytes)}"` (note is the existing trend label when `audiobook_stats` absent). Audiobooks metric (only when `audiobook_stats`): label "Audiobooks", value count, note `"${hours} h total · +${added_last_30_days} in 30 days"` with hours = `Math.round(duration_seconds/3600)`. Disk metric: label "Disk", value `"${formatSize(free)} free"` or "Unknown", note `"of ${formatSize(total)}"`, accent `"red"` when `low` else `"slate"`, plus `meter: used/total`.

- [ ] **Step 1: Failing tests** in `admin-dashboard.test.cjs`:
  - `"storage card totals memes and audiobooks"`: dashboard `{total_size_bytes:1024, total_storage_bytes:3072, audiobook_stats:{total_bytes:2048, count:2, duration_seconds:7200, added_last_30_days:1, series:[]}, metric_series:[], counts:{}}` → Storage metric value `"3 KB"` (match whatever `formatSize(3072)` returns — assert via `context.formatSize(3072)`), note contains `"Audiobooks"`; an Audiobooks metric exists with value `"2"` and note starting `"2 h total"`.
  - `"audiobook card hidden without stats"`: no `audiobook_stats` → no metric labelled "Audiobooks".
  - `"disk card shows Unknown and low"`: no `disk` → Disk value `"Unknown"`; `disk:{total_bytes:100, free_bytes:5, used_bytes:95, low:true}` → accent `"red"`.
  - `"empty dashboard produces no NaN"`: `{counts:{}, metric_series:[]}` → `JSON.stringify(metrics)` does not contain `NaN`.
- [ ] **Step 2: Run** `node --test scripts/admin-dashboard.test.cjs` — FAIL.
- [ ] **Step 3: Implement** the builder, make `renderAdminDashboard` call it, render the disk meter, combine the storage sparkline by adding `audiobook_stats.series[i].bytes` to `metric_series[i].storage_bytes` by date.
- [ ] **Step 4: Run** — PASS. Manually check with the app running (`run` skill): 8 cards at desktop, wraps at 375px wide with no horizontal scroll.
- [ ] **Step 5: Commit** `Show combined storage, audiobooks, and disk cards on the dashboard`. **Open Phase 1 PR.**

---

## Phase 2 — New dashboard panels

### Task 6: Listening activity, audiobook health, and work queues (backend)

**Files:**
- Create: `internal/client/admin_audiobook_activity.go`
- Modify: `internal/client/audiobook_store.go` (interface methods), `internal/client/server.go` (response + handler)
- Test: `internal/client/admin_audiobook_activity_test.go`

**Interfaces:**
- Produces (JSON tags exactly as in the spec Phase 2 block):
  `adminAudiobookActivity{DailySeconds []adminDaySeconds; TopBooks []adminTopBook; TopListeners []adminTopListener; Health adminAudiobookHealth}`,
  `adminDaySeconds{Date string "date"; Seconds float64 "seconds"}`,
  `adminTopBook{ID, Title string "id","title"; Seconds float64 "seconds"}`,
  `adminTopListener{UserID, DisplayName string "user_id","display_name"; Seconds float64 "seconds"}`,
  `adminWorkQueues{DeleteQueue, SuggestedTags, LinkRetries, RejectedLinks, ConversionQueue int}`.
  `audiobookStore.AdminListening(ctx, now time.Time) (daily []adminDaySeconds, books []adminTopBook, listeners []adminTopListener, err error)` — 30 days, top 5 each, listener `DisplayName` empty (handler fills from `s.users.ListUsers`, falling back to user id).
  `audiobookStore.AdminHealthCounts(ctx) (adminAudiobookHealth, error)` — `MissingCover`: `cover_path = ''`; `MissingChapters`: `chapters = '[]'::jsonb`; `MissingSeries`: `series_id IS NULL`; `AwaitingConversion`: `len(ListStoredMP3s)`; `FailedConversion`: those MP3s whose `audiobookRemuxFailurePath(storage_path)` exists (compute in the handler or a helper, not SQL).
  `adminDashboardResponse` gains `AudiobookActivity *adminAudiobookActivity \`json:"audiobook_activity,omitempty"\`` and `WorkQueues adminWorkQueues \`json:"work_queues"\``. Work queue sources: pending-delete total (the source behind `handlePendingDeleteQueue`), pending-suggestion count (`stats.PendingSuggestionMemes`), `s.linkRetries` queued/rejected lengths (the source behind `handleLinkDownloadStatus`), `ConversionQueue = AwaitingConversion`.

- [ ] **Step 1: Failing test** `TestAdminListening` (Postgres): two users, two books; stats rows: u1/b1 today 600s, u1/b1 3 days ago 300s, u2/b2 today 100s, u2/b2 45 days ago 999s. Assert `len(daily)=30`, `daily[29].Seconds=700`, books `[b1:900, b2:100]` in that order, listeners `[u1:900, u2:100]`.
- [ ] **Step 2: Failing test** `TestAdminHealthCounts` (Postgres): books with/without cover, chapters `[]` vs one chapter, series null vs set → exact counts.
- [ ] **Step 3: Failing test** `TestAdminDashboardWorkQueues`: `newShareTestServer` with one meme marked pending delete (use the existing helper the delete-queue tests use) → `work_queues.delete_queue == 1`.
- [ ] **Step 4: Run** — FAIL. **Step 5: Implement.** **Step 6: Run** — PASS.
- [ ] **Step 7: Commit** `Add listening activity, audiobook health, and work queue counts to the dashboard API`.

### Task 7: Dashboard panels (frontend)

**Files:**
- Modify: `static/index.html` (three new sections in `#admin-dashboard-panel`: `#admin-dashboard-queues` strip above `.admin-dashboard-analytics`; `#admin-dashboard-listening` and `#admin-dashboard-audiobook-health` sections in a new `.admin-dashboard-audiobooks` row), `static/app.js`, `static/styles.css`
- Test: `scripts/admin-dashboard.test.cjs`

**Interfaces:**
- Consumes: Task 6 JSON.
- Produces (pure, above `renderAdminDashboard`): `buildAdminWorkQueues(queues) -> html` (one `<button class="admin-queue-chip" data-admin-open-tab="<tab>" data-empty="<bool>">` per queue: delete-queue, tag-review, link-retries, rejected-links, audiobook-conversion), `buildAdminListening(activity) -> html` (reuses `buildAdminUploadChart` by mapping `{date, uploads: Math.round(seconds/60)}` and labelling minutes), `buildAdminAudiobookHealth(health) -> html` (buttons with `data-admin-open-tab="audiobook-library" data-admin-library-filter="no-cover|no-chapters|no-series|not-converted|conversion-failed"`).
  `adminTabBadgeCounts()` prefers `state.admin.dashboard?.work_queues` when present.

- [ ] **Step 1: Failing tests**: `"work queue chips link to their tabs"` (five chips, correct `data-admin-open-tab`, zero → `data-empty="true"`); `"health buttons carry library filters"` (five filters present); `"listening chart handles empty activity"` (`{daily_seconds:[], top_books:[], top_listeners:[]}` → no `NaN`, contains "No listening yet").
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement**; wire `[data-admin-open-tab]` clicks to switch tabs (and set `state.admin.audiobookLibrary.filter` from `data-admin-library-filter`, consumed in Task 12). Hide the audiobook row when `audiobook_activity` is absent. **Step 4: Run** — PASS; check in the running app.
- [ ] **Step 5: Commit** `Add work queue, listening activity, and audiobook health panels`. **Open Phase 2 PR.**

---

## Phase 3 — Storage page

### Task 8: Storage scan (breakdown, largest files, orphans)

**Files:**
- Create: `internal/client/admin_storage.go`
- Test: `internal/client/admin_storage_test.go`

**Interfaces:**
- Produces:
  `type storageDirUsage struct { Name string "name"; Path string "path"; Bytes int64 "bytes"; Files int "files" }`
  `func measureStorage(dataDir string) ([]storageDirUsage, error)` — entries in this order: `uploads`, `thumbnails`, `audiobooks` (excluding `covers` and `tmp` subtrees), `audiobooks/covers`, `audiobooks/tmp`, `downloads`, `backups`, `other` (everything else at top level). `WalkDir`, symlinks counted as 0 and not followed; missing dirs → 0.
  `type storageReferences struct { Paths map[string]struct{} }` — absolute, cleaned paths referenced by the DB.
  `type orphanFile struct { Path string "path"; Dir string "dir"; Bytes int64 "bytes"; ModifiedAt time.Time "modified_at" }`
  `type missingFile struct { Kind string "kind" /* meme|audiobook|cover */; ID, Title, Path string }`
  `func findOrphans(dataDir string, refs storageReferences, now time.Time) ([]orphanFile, error)` — scans `uploads`, `thumbnails`, `audiobooks` (top level only), `audiobooks/covers`, `audiobooks/tmp`; skips mtime within 1h of `now`, names ending in the remux failure suffix, symlinks, directories.
  `func (s *Server) collectStorageReferences(ctx) (storageReferences, []missingFile, error)` — meme `FilePath`/`PreviewPath` mapped from `/uploads/<name>` and preview URL paths to files under `s.managers.UploadDir()` / `ThumbnailDir()`; audiobook `storage_path`, `cover_path`; `audiobook_uploads.temp_path`. Rows whose file is missing go in `[]missingFile`. Needs `audiobookStore.ListStoragePaths(ctx) ([]audiobookStoragePath, error)` with `{ID, Title, StoragePath, CoverPath string}` and `ListUploadTempPaths(ctx) ([]string, error)`.

- [ ] **Step 1: Failing test** `TestMeasureStorage`: temp tree with files of known sizes in each dir including `audiobooks/covers/a.jpg` (3), `audiobooks/book.m4b` (10), `notes.txt` at top (2), a symlink in `uploads` → `audiobooks` entry = 10 (not 13), `other` = 2, symlink not counted.
- [ ] **Step 2: Failing test** `TestFindOrphans`: referenced `uploads/a.png`; unreferenced `uploads/b.png` (old mtime via `os.Chtimes`), `uploads/c.png` (mtime now), `audiobooks/x.mp3.remux-failed` (whatever suffix `audiobookRemuxFailurePath` produces), symlink `uploads/link.png`, `backups/p1.part`, unreferenced `audiobooks/tmp/u1` → result is exactly `uploads/b.png` and `audiobooks/tmp/u1`.
- [ ] **Step 3: Run** — FAIL. **Step 4: Implement.** **Step 5: Run** — PASS.
- [ ] **Step 6: Commit** `Add storage breakdown and orphan detection`.

### Task 9: Storage endpoints, orphan delete, upload discard

**Files:**
- Modify: `internal/client/admin_storage.go`, `internal/client/server.go` (routes), `internal/client/audiobook_handlers.go` (extract sweeper body)
- Modify: `internal/client/deep_security_test.go` (route list)
- Test: `internal/client/admin_storage_handlers_test.go`

**Interfaces:**
- Consumes: Task 8 functions, `diskUsage`.
- Produces:
  `GET /api/admin/storage[?refresh=1]` → `{ "dirs": []storageDirUsage, "disk": adminDiskStats|null, "largest": []storageLargeFile, "uploads": []adminUploadInProgress, "generated_at": time }`; cached 5 min on `Server`.
  `storageLargeFile{Kind "meme"|"audiobook"; ID; Name; Bytes; Owner string}` — top 25 by DB `size_bytes` across memes and audiobooks.
  `adminUploadInProgress{ID; Title; UserID; DisplayName; ReceivedBytes; TotalBytes int64; UpdatedAt time.Time}`.
  `POST /api/admin/storage/orphans/scan` → `{ "orphans": []orphanFile, "missing": []missingFile, "scanned_at": time }`; result kept on `Server` as the delete allow-list.
  `POST /api/admin/storage/orphans/delete` body `{"paths":[]string}` → `{ "results": [{ "path", "deleted": bool, "error": string }] , "bytes_freed": int64 }`. Each path must (a) be in the last scan's orphan set, (b) `filepath.Clean` + `filepath.Rel(dataDir, p)` not starting with `..`, (c) `Lstat` is a regular file, (d) still unreferenced after re-running `collectStorageReferences`. One server log line per batch: `storage cleanup by <actor>: N files, B bytes`.
  `DELETE /api/admin/audiobooks/uploads/{id}` → 204; uses `func (s *Server) discardAudiobookUpload(ctx, upload audiobookUpload) error` extracted from `startAudiobookUploadCleanupSweeper` (lock, remove temp file, `DiscardUpload`, `audiobookUploadLocks.Delete`); the sweeper calls the same helper. Requires an admin variant of looking up an upload without the owner check: `audiobookStore.GetUploadByID(ctx, id) (audiobookUpload, error)`.

- [ ] **Step 1: Failing tests:**
  - `TestStorageOrphanDeleteRefusesUnscannedAndEscaping`: after a scan returning `uploads/b.png`, POST delete with `["<dataDir>/uploads/b.png", "<dataDir>/../outside.txt", "<dataDir>/uploads/a.png", "/etc/hostname"]` → only b.png `deleted:true`; others `deleted:false` with non-empty `error`; `outside.txt` (created in the parent temp dir) and `a.png` still exist.
  - `TestStorageOrphanDeleteRechecksReferences`: scan finds `b.png`; then a meme row referencing `b.png` is created; delete → `deleted:false`.
  - `TestStorageEndpointWithoutPostgres`: GET `/api/admin/storage` → 200, `uploads` is `[]`.
  - Add `{"GET","/api/admin/storage"}`, `{"POST","/api/admin/storage/orphans/scan"}`, `{"POST","/api/admin/storage/orphans/delete"}`, `{"DELETE","/api/admin/audiobooks/uploads/x"}` to `TestActualRoutesDenyViewerAdministrativeActions`.
- [ ] **Step 2: Run** `go test ./internal/client/ -run 'TestStorage|TestActualRoutesDeny' -v` — FAIL.
- [ ] **Step 3: Implement.** **Step 4: Run** — PASS; existing sweeper tests still pass.
- [ ] **Step 5: Commit** `Add storage admin endpoints with guarded orphan cleanup`.

### Task 10: Storage page UI

**Files:**
- Create: `static/admin-pages.js`
- Modify: `static/index.html` (script tag; `<section id="admin-storage-panel" class="admin-storage-panel hidden">` with `#admin-storage-dirs`, `#admin-storage-largest`, `#admin-storage-orphans`, `#admin-storage-uploads`, Refresh and Scan buttons), `static/app.js` (`ADMIN_TABS.storage` entry + `load`, and a hook so `renderAdminView` calls `window.renderAdminStorage?.()`), `static/styles.css`
- Test: `scripts/admin-pages.test.cjs` (new)

**Interfaces:**
- Produces (in `admin-pages.js`, pure and top-level so the test can `vm.runInContext` the whole file with stubbed `escapeHTML`/`formatSize`/`formatRelativeTime` from `app.js` slices): `buildStorageDirs(dirs, disk) -> html` (one row per dir with a proportional bar, sorted by bytes desc, `other` last), `buildStorageOrphans(scan, selected:Set) -> html` (checkbox per orphan, total selected bytes in a footer, "Missing files" sub-table listing `missing`), `buildStorageUploads(uploads) -> html` (`<button data-discard-upload="<id>">`).
  `renderAdminStorage()`, `loadAdminStorage({refresh})`, state at `state.admin.storage = {data, scan, selected, busy}`.
  `ADMIN_TABS.storage = { group:"Operations", label:"Storage", superOnly:true, panel:"admin-storage-panel", title:"Storage", kicker:"Admin", copy:"See what is using disk space, find files the database no longer references, and clear abandoned uploads." }`

- [ ] **Step 1: Failing tests** in `admin-pages.test.cjs`: `"admin pages script parses"`; `"storage dirs sort by size with other last"`; `"orphan footer totals selected bytes"` (two orphans 1024 + 2048, both selected → footer contains `formatSize(3072)`); `"orphan list escapes file names"` (path `uploads/<img onerror=x>.png` → no raw `<img` in output).
- [ ] **Step 2: Run** `node --test scripts/admin-pages.test.cjs` — FAIL.
- [ ] **Step 3: Implement.** Delete confirmation: `confirm()` text `"Delete ${n} files (${formatSize(bytes)})? This cannot be undone."`; after delete, show per-path failures in the panel status line and re-scan. Discard upload confirms `"Discard the unfinished upload of “${title}”?"`.
- [ ] **Step 4: Run** — PASS; exercise in the running app (scan, select, delete a planted test file).
- [ ] **Step 5: Commit** `Add Storage admin page`. **Open Phase 3 PR.**

---

## Phase 4 — Audiobook Library page

### Task 11: Admin audiobook list endpoint

**Files:**
- Create: `internal/client/admin_audiobook_library.go`
- Modify: `internal/client/audiobook_store.go`, `internal/client/server.go`, `internal/client/deep_security_test.go`
- Test: `internal/client/admin_audiobook_library_test.go`

**Interfaces:**
- Produces: `GET /api/admin/audiobooks?sort=<col>&dir=asc|desc&filter=<f>&page=<n>&page_size=<n>` → `{ "items": []adminAudiobookRow, "total": int, "page": int, "page_size": int }`; `page_size` default 50, max 200; unknown `sort` → `created_at desc`; unknown `filter` → 400.
  Sort columns: `title, author, format, size, duration, uploader, created_at, listeners, hours`.
  Filters: `no-cover, no-chapters, no-series, not-converted, conversion-failed` (same definitions as Task 6; `not-converted` = mime/extension is MP3; `conversion-failed` = MP3 with a failure marker — filter in Go after the SQL page query would break paging, so compute the set of failed ids first from `ListStoredMP3s` + marker stat and pass it as `id = ANY($n)`).
  `adminAudiobookRow{ID, Title, Author, Format ("MP3"|"M4B"|other upper-cased ext), SizeBytes, DurationSeconds, UploaderID, UploaderName, CreatedAt, HasCover, HasChapters, HasSeries bool, ActiveListeners int /* distinct users with listening_stats in 30d */, SecondsListened float64 /* all time */}`.
  `audiobookStore.AdminListAudiobooks(ctx, query adminAudiobookQuery) ([]adminAudiobookRow, int, error)` with `adminAudiobookQuery{Sort, Dir, Filter string; FailedIDs []string; Limit, Offset int}`. Sort column names are mapped through a fixed Go map to SQL expressions — never interpolated from input.

- [ ] **Step 1: Failing tests** (Postgres): `TestAdminAudiobooksSortBySize` (3 books, `sort=size&dir=desc` → sizes descending); `TestAdminAudiobooksFilterNoCover`; `TestAdminAudiobooksPaging` (5 books, `page_size=2&page=3` → 1 item, `total=5`); `TestAdminAudiobooksRejectsUnknownFilter` (400); `TestAdminAudiobooksSortInjection` (`sort=title;DROP TABLE audiobooks` → 200, default order, table still exists). Add the route to the deny-viewer test.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add admin audiobook library endpoint`.

### Task 12: Audiobook Library page UI

**Files:**
- Modify: `static/admin-pages.js`, `static/index.html` (`#admin-audiobook-library-panel` with filter `<select id="admin-library-filter">`, table container, reuse `#admin-page-prev/next`), `static/app.js` (`ADMIN_TABS["audiobook-library"]`, new group "Audiobooks", move `audiobook-conversion` to that group), `static/styles.css`
- Test: `scripts/admin-pages.test.cjs`

**Interfaces:**
- Consumes: Task 11 endpoint; `state.admin.audiobookLibrary.filter` set by Task 7's health buttons.
- Produces: `buildAudiobookLibraryTable(rows, sort) -> html` (sortable `<button data-library-sort="<col>" aria-sort>` headers; ✓/— cells for cover/chapters/series; rows `<button data-admin-audiobook-id>`), `renderAdminAudiobookLibrary()`, `loadAdminAudiobookLibrary()`. Row click calls the existing audiobook edit-modal opener in `app.js` (find the function the audiobook detail view uses to open the edit modal and call it by id; if it needs a full record, fetch `/api/audiobooks/{id}` first). Without Postgres the panel shows "Requires PostgreSQL".
  `ADMIN_TABS["audiobook-library"] = { group:"Audiobooks", label:"Library", superOnly:true, panel:"admin-audiobook-library-panel", title:"Audiobook Library", kicker:"Admin", copy:"Every audiobook on the server with its size, format, metadata gaps, and how much it is listened to." }`

- [ ] **Step 1: Failing tests**: `"library header marks the active sort"` (`aria-sort="descending"` on size); `"library rows show metadata gaps"` (row with `has_cover:false` renders the gap marker in the cover cell); `"library escapes titles"`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS; in the app, click a health count on the dashboard and confirm the library opens filtered.
- [ ] **Step 5: Commit** `Add Audiobook Library admin page`. **Open Phase 4 PR.**

---

## Phase 5 — User Activity page

### Task 13: Audit-log actor filter

**Files:**
- Modify: `internal/accessor` audit search (the filter struct passed where `Search: r.URL.Query().Get("q")` is built in `handleAuditLogs`, `internal/client/server.go` ~line 2125) — add `ActorUserID string` and a `actor_user_id = $n` clause in the Postgres query and an equality check in the JSON store.
- Test: extend the existing audit-log tests (`grep -rn "ListAudit" internal --include=*_test.go` to find them) with `TestAuditLogsFilterByActor`.

**Interfaces:**
- Produces: `GET /api/admin/audit-logs?actor=<user_id>` returns only that actor's events; combines with existing `q`/`view`.

- [ ] **Step 1: Failing test** `TestAuditLogsFilterByActor`: two memes uploaded by actors `a` and `b` → `?actor=a` returns only `a`'s events. **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS. **Step 5: Commit** `Filter audit logs by actor`.

### Task 14: User activity endpoint and page

**Files:**
- Create: `internal/client/admin_user_activity.go`
- Modify: `internal/client/audiobook_store.go`, `internal/client/server.go`, `internal/client/deep_security_test.go`, `static/admin-pages.js`, `static/index.html` (`#admin-user-activity-panel`), `static/app.js` (`ADMIN_TABS["user-activity"]`; Audit Logs tab shows an actor chip `<button data-clear-audit-actor>` when `state.admin.auditActor` is set and passes `actor=` to the fetch)
- Test: `internal/client/admin_user_activity_test.go`, `scripts/admin-pages.test.cjs`

**Interfaces:**
- Produces: `GET /api/admin/user-activity` → `{ "users": []adminUserActivity }`.
  `adminUserActivity{UserID, DisplayName, Username, AvatarURL string; MemesUploaded, AudiobooksUploaded, Favorites int; Seconds30d, SecondsAllTime float64; BooksFinished int; LastActiveAt int64}`.
  Sources: users from `s.users.ListUsers`; memes uploaded = audit events with `action='uploaded'` grouped by `actor_user_id` (new manager/accessor method `CountAuditActionsByActor(action string) (map[string]int, error)`); favorites from `user_favorites` grouped by `user_id` (`CountFavoritesByUser() (map[string]int, error)`); audiobook counts via `audiobookStore.AdminUserAudiobookTotals(ctx, since time.Time) (map[string]adminUserAudiobookTotals, error)` with `{Uploaded int; Seconds30d, SecondsAllTime float64; Finished int}` (uploaded from `audiobooks.uploaded_by_user_id`, finished from `audiobook_progress.finished_at IS NOT NULL`). Without Postgres audiobook fields are 0. Users missing from `ListUsers` but present in counts are omitted.
  Frontend: `buildUserActivityTable(users, sort) -> html`, client-side sort, row `<button data-user-activity-id>` → set `state.admin.auditActor`, switch to `audit-logs` tab.
  `ADMIN_TABS["user-activity"] = { group:"Overview", label:"User Activity", superOnly:true, panel:"admin-user-activity-panel", sibling:"users", title:"User Activity", kicker:"Admin", copy:"Who uploads, favorites, and listens - and when each account was last active." }`; also set `users.sibling = "user-activity"`.

- [ ] **Step 1: Failing tests:** `TestAdminUserTotals` (Postgres: u1 uploaded 2 books, finished 1, listened 600s today and 900s 40 days ago → `{Uploaded:2, Seconds30d:600, SecondsAllTime:1500, Finished:1}`); `TestUserActivityWithoutPostgres` (`newShareTestServer` → 200, audiobook fields 0); deny-viewer route entry; JS `"user activity sorts by listening hours"`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS; in the app click a user and confirm Audit Logs filters, and the chip clears it.
- [ ] **Step 5: Commit** `Add User Activity admin page`. **Open Phase 5 PR.**

---

## Phase 6 — Series & audiobook tags

### Task 15: Series merge, empty-only delete, tag usage

**Files:**
- Modify: `internal/client/audiobook_store.go`, `internal/client/audiobook_handlers.go` (`handleAudiobookSeriesByID` DELETE ~line 930), `internal/client/server.go` (routes), `internal/client/deep_security_test.go`
- Create: `internal/client/admin_audiobook_series.go`
- Test: `internal/client/admin_audiobook_series_test.go`

**Interfaces:**
- Produces:
  `POST /api/admin/audiobook-series/{id}/merge` body `{"into":"<id>"}` → `{ "moved": int }`; 400 if `into == id` or missing; 404 if either series missing. `audiobookStore.MergeSeries(ctx, fromID, intoID string) (int, error)` — one transaction: `UPDATE audiobooks SET series_id=$into WHERE series_id=$from` (sequence numbers kept), `DELETE FROM audiobook_series WHERE id=$from`.
  Existing `DELETE /api/audiobook-series/{id}` is unchanged: `DeleteSeries` removes the row and `ON DELETE SET NULL` unassigns its books. It is a user-facing feature, so the admin page reuses it and warns with the book count instead of blocking.
  `GET /api/admin/audiobook-tags` → `{ "tags": [{ "tag", "audiobooks": int, "memes": int, "restricted_users": [{"user_id","display_name"}] }] }` sorted by audiobooks desc.

- [ ] **Step 1: Failing tests** (Postgres): `TestMergeSeriesMovesBooksKeepingSequence` (A has books seq 1,2; B has seq 3 → after merge B has 3 books with sequences 1,2,3 and A is gone); `TestMergeSeriesIntoSelfRejected` (handler 400); `TestAdminAudiobookTagUsage` (tag on 2 books + 1 meme, one restricted user). Deny-viewer route entries for both admin routes.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add series merge and audiobook tag usage endpoints`.

### Task 16: Series & Tags page UI

**Files:**
- Modify: `static/admin-pages.js`, `static/index.html` (`#admin-audiobook-series-panel` with `#admin-series-list` and `#admin-audiobook-tags`), `static/app.js` (`ADMIN_TABS["audiobook-series"]`), `static/styles.css`
- Test: `scripts/admin-pages.test.cjs`

**Interfaces:**
- Consumes: existing `GET /api/audiobook-series` (list with `BookCount`), existing `PATCH`/`DELETE /api/audiobook-series/{id}` (rename/delete), Task 15 endpoints, and the existing per-book endpoint backed by `SetAudiobookSeries` (the one the edit modal uses) for `series_sequence`.
- Produces: `buildSeriesList(series, expandedID, books) -> html` (rename inline input, "Merge into…" `<select>` of other series, Delete button whose confirm reads `"Delete “${name}”? Its ${n} books will no longer be in a series."` (plain `"Delete “${name}”?"` when empty), expanded view lists books with `<input type="number" step="0.5" data-series-sequence="<bookId>">`), `buildAudiobookTagUsage(tags) -> html` (restricted users shown as chips; each tag links to Tag Hygiene with the tag prefilled in `#admin-tag-merge-source`).
  `ADMIN_TABS["audiobook-series"] = { group:"Audiobooks", label:"Series & Tags", superOnly:true, panel:"admin-audiobook-series-panel", title:"Series & Tags", kicker:"Admin", copy:"Rename, merge, and reorder audiobook series, and see which tags audiobooks and restricted accounts depend on." }`
  Merge confirms `"Move ${n} books from “${from}” into “${into}” and delete “${from}”?"`.

- [ ] **Step 1: Failing tests**: `"series delete confirm mentions book count"` (export the confirm-text builder `seriesDeleteConfirmText(series)` and assert both forms); `"merge select excludes the series itself"`; `"tag usage lists restricted users"`; `"series names are escaped"`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS; in the app rename, reorder, and merge two test series.
- [ ] **Step 5: Commit** `Add Series & Tags admin page`. **Open Phase 6 PR.**

---

## Final check (after Phase 6)

- [ ] `go test ./...` with `MEMEINDEX_TEST_DATABASE_URL` set, and `node --test scripts/` — all pass.
- [ ] In the running app at desktop and 375px width: every new tab renders, rail groups read Overview / Tags / Imports / Operations / Audiobooks, no horizontal scroll.
- [ ] With `DatabaseURL` unset: dashboard renders, audiobook cards/panels hidden, audiobook pages show "Requires PostgreSQL", Storage page works.
