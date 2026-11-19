# Go Code Health Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Go lint backlog to zero with CI gating on every finding, then split the three largest Go files by area without changing any code.

**Architecture:** Two stacked PRs. PR 1 (`go-lint-backlog`) fixes line endings, fixes all 78 findings package by package, and turns on full lint gating. PR 2 (`go-file-splits`, branched from PR 1) moves declarations into area files in the same package, proven unchanged by a declaration-hash check.

**Tech Stack:** Go 1.27.3, golangci-lint v2.15.0, pgx v5.12, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-13-go-code-health-design.md`

## Global Constraints

- `$LINT` means `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run` (the version CI pins).
- `$SCRATCH` is a scratch directory outside the repo for throwaway tools; nothing from it is committed.
- No behavior changes beyond what a lint fix requires. No new packages, no renamed or newly exported identifiers, no API or schema changes, nothing under `static/`.
- Deliberately ignored errors are written `_ = f()` or, when deferred, `defer func() { _ = f() }()`. No errcheck `exclude-functions`, no config exclusions for real findings.
- An error that carries a cause callers branch on (`tagsuggest.ErrUnavailable`, `ErrInvalidResponse`, `ErrUnsupported`, `errAutoSuggestExhausted`, `os.ErrNotExist`) is formatted as `%s` with `.Error()`, never wrapped a second time with `%w`. Only causes nobody branches on get `%w`.
- Local tests run as `go test ./...` (the race detector needs cgo, which this Windows machine lacks; CI runs `-race`).
- Commit messages follow the repo's style (a plain sentence) and end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. PR descriptions end with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

## Review Focus

- A tag-suggestion run whose image file is missing must stay "unavailable" (retried later), not become "not found" (dropped from the queue) → `TestSuggestMissingAssetStaysUnavailable` in Task 3.
- Two uploads of the same file racing each other must still answer "duplicate" when pgx wraps the unique-violation error → `TestIsUniqueViolation` in Task 2.
- Restoring a backup whose tar entries use the legacy `'\x00'` regular-file type must still extract them after `tar.TypeRegA` is dropped → `TestExtractPortableArchiveAcceptsLegacyRegularFileType` in Task 5.
- A failed final write of an uploaded file (disk full) must not leave a meme pointing at a short file. No portable test can force a `Close` failure; the reviewer checks Task 2's code path removes the file and returns the error.
- A pure move that drops a doc comment or a section comment → `declcheck` (Task 7) hashes doc comments with their declaration and lists free-floating comments separately.

---

## PR 1: line endings and lint backlog (branch `go-lint-backlog`)

### Task 1: Branch setup, LF line endings, full lint gating

**Files:**
- Modify: `.gitattributes`, `.golangci.yml`, `.github/workflows/ci.yml` (lint job)
- Carry over from the main checkout: `README.md` ("Next ideas" refresh), `docs/superpowers/specs/2026-11-13-go-code-health-design.md`, this plan

**Interfaces:**
- Produces: a branch on which `$LINT ./...` reports every finding (no merge-base filter) and `gofmt -l .` is empty.

- [ ] **Step 1: Create the worktree** with superpowers:using-git-worktrees: branch `go-lint-backlog` from `master`.
- [ ] **Step 2: Carry over the uncommitted docs.** Copy `README.md`, the spec and this plan from the main checkout into the worktree. In the main checkout, run `git checkout -- README.md` and delete the spec and plan files there, so a later `git pull` is not blocked. Leave `static/app.js` and `static/styles.css` in the main checkout untouched.
- [ ] **Step 3: Add LF rules to `.gitattributes`:** `*.go text eol=lf`, `go.mod text eol=lf`, `go.sum text eol=lf`, with a one-line comment that local gofmt must see what CI sees.
- [ ] **Step 4: Rewrite the working copies.** Delete every file from `git ls-files '*.go' go.mod go.sum` and run `git checkout -- .`.
  Verify: `git ls-files --eol '*.go' | grep -c w/crlf` prints `0`, and `gofmt -l .` prints nothing. If `gofmt -l` still lists files, those have real formatting drift: run `gofmt -w` on exactly those files and commit them in this task.
- [ ] **Step 5: Gate CI on every finding.** In `.golangci.yml`, delete `new-from-merge-base: origin/master` and the comment above it. Keep `max-issues-per-linter: 0` and `max-same-issues: 0`. In `ci.yml`, drop the lint job's `fetch-depth: 0` and its comment.
- [ ] **Step 6: Record the baseline.** Run `$LINT ./...`. Expected: 78 issues (errcheck 34, staticcheck 28, errorlint 9, misspell 4, unused 2, bodyclose 1).
- [ ] **Step 7: Commit:** "Check Go files out with LF and gate CI on every lint finding" (also adds the README refresh, spec and plan).

### Task 2: `internal/accessor` lint-clean

**Files:**
- Modify: `internal/accessor/postgres_store.go` (lines 691, 1195, 1205, 1213, 1256, 1319, 1661, 1768), `internal/accessor/meme_store.go` (465, 502), `internal/accessor/dedupe.go` (60)
- Test: `internal/accessor/postgres_unique_test.go` (new)

**Interfaces:**
- Produces: `func isUniqueViolation(err error) bool` in `postgres_store.go`.

- [ ] **Step 1: Write the failing test** `TestIsUniqueViolation`, table-driven:
  - `&pgconn.PgError{Code: "23505"}` → true
  - `fmt.Errorf("insert meme: %w", &pgconn.PgError{Code: "23505"})` → true
  - `&pgconn.PgError{Code: "23503"}` → false
  - `errors.New("boom")` → false
  - `nil` → false
- [ ] **Step 2: Run** `go test ./internal/accessor -run TestIsUniqueViolation`. Expected: FAIL, `undefined: isUniqueViolation`.
- [ ] **Step 3: Implement `isUniqueViolation`** with `errors.As` into `*pgconn.PgError`, and use it at line 1213. Also replace any other `err.(*pgconn.PgError)` assertion in the package (`grep -n '(\*pgconn.PgError)' internal/accessor`).
- [ ] **Step 4: Check the upload file's close in `meme_store.go` `Create`** (line 465). Keep the deferred close for early returns (as `_ =`). After the copy succeeds, close explicitly; on error, remove `targetPath` and return `fmt.Errorf("close upload file: %w", err)`.
- [ ] **Step 5: Fix the rest.**
  - The six `tx.Rollback` defers and the `dedupe.go` close become `_ =` forms.
  - The two empty `if err := ensurePreviewAsset(...); err != nil {}` branches (meme_store.go:502, postgres_store.go:1195) become `_ = ensurePreviewAsset(...)` under the existing "best-effort" comment.
- [ ] **Step 6: Verify.** `$LINT ./internal/accessor/...` reports 0 issues, and `go test ./internal/accessor/...` passes.
- [ ] **Step 7: Commit:** "Fix accessor lint findings and spot unique violations through wrapped errors".

### Task 3: `internal/tagsuggest` lint-clean

**Files:**
- Modify: `internal/tagsuggest/service.go` (lines 182, 184, 223, 294, 322, 364, 384, 400, 402, 643–647)
- Test: `internal/tagsuggest/service_test.go`

**Interfaces:**
- Consumes: `New(Config) *Service`, `(*Service).ready(ctx) (bool, error)`, `(*Service).Suggest(ctx, Request) (Result, error)`. Build the service the way `TestGenerateEndpointUsesSchemaAndCleansResponse` does.

- [ ] **Step 1: Write the tests.**
  - `TestReadyKeepsTransportCause`: start an `httptest.Server`, close it, and point a service at it. Then `_, err := s.ready(ctx)`. Assert `errors.Is(err, ErrUnavailable)`, and that `errors.As(err, &urlErr)` succeeds with `var urlErr *url.Error`.
  - `TestSuggestMissingAssetStaysUnavailable`: on an enabled service, call `Suggest` with `Request{AssetPaths: []string{filepath.Join(t.TempDir(), "missing.png")}}`. Assert `errors.Is(err, ErrUnavailable)` is true and `errors.Is(err, os.ErrNotExist)` is **false**.
- [ ] **Step 2: Run** `go test ./internal/tagsuggest -run 'TestReadyKeepsTransportCause|TestSuggestMissingAssetStaysUnavailable'`. Expected: the first FAILs (the cause is dropped by `%v`); the second passes now and guards the change.
- [ ] **Step 3: Rewrite the `fmt.Errorf` calls.**
  - Lines 182, 322 and 400 wrap their cause with `%w` (e.g. `"%w: %w"`).
  - Line 223 formats the read error as `%s` with `err.Error()`. Wrapping it would make the worker drop the meme as "not found".
  - Line 384 formats `originalErr.Error()` as `%s`.
  - Line 402 becomes `"%w: structured path failed (%s); fallback parse failed: %w"` with `ErrInvalidResponse, originalErr.Error(), err`.
  - This matches the existing `originalErr.Error()` pattern near line 362.
- [ ] **Step 4: Fix the rest.** The three `response.Body.Close` defers become `_ =` forms. Lines 643–647 use `fmt.Fprintf(&builder, ...)`.
- [ ] **Step 5: Verify.** Both tests pass, `$LINT ./internal/tagsuggest/...` reports 0 issues, and `go test ./internal/tagsuggest/...` passes.
- [ ] **Step 6: Commit:** "Keep transport causes on tag-suggestion errors and fix tagsuggest lint findings".

### Task 4: `internal/manager` lint-clean

**Files:**
- Modify: `internal/manager/meme_manager.go` (line 2433; delete `looksLikeTagVariant` at 2505)
- Modify tests: `admin_read_cache_test.go` (122, 133), `tag_ops_delegation_test.go` (165, 177), `admin_dashboard_memes_test.go` (49, 50, 71, 72), `bulk_meme_store_test.go` (43, 44), `meme_query_delegation_test.go` (68, 69, 110, 133, 134, 157, 158)

- [ ] **Step 1: Keep the exhausted error's single classification.** Line 2433 becomes `fmt.Errorf("%w: %s", errAutoSuggestExhausted, lastInvalidResponse.Error())`. Wrapping it would make it also match `ErrInvalidResponse`.
- [ ] **Step 2: Delete `looksLikeTagVariant`.** `grep -rn looksLikeTagVariant internal` must then show nothing.
- [ ] **Step 3: Mark the deliberate typo.** `buisness` is a fixture for tag-variant detection, so keep it. Add `//nolint:misspell // deliberate typo: tag-variant fixture` to those four lines.
- [ ] **Step 4: Fix the test selectors.** `x.adminReadCacheStore.Method` becomes `x.Method` at the 13 QF1008 sites.
- [ ] **Step 5: Verify.** `$LINT ./internal/manager/...` reports 0 issues, and `go test ./internal/manager/...` passes.
- [ ] **Step 6: Commit:** "Fix manager lint findings".

No new test: the manager tests have no fake tag suggester to drive `suggestTagsForMeme`. Step 1 keeps the existing message and classification.

### Task 5: `internal/client` lint-clean

**Files:**
- Modify: `auth.go` (776, 804; delete `buildLoginURL` at 170), `direct_media.go` (53, 96, 112, 113, 129), `migration.go` (345, 456, 460), `portable_backup.go` (473, 704, 829, 897, 1126, 1158), `server.go` (364, 383, 668, 749, 1468, 1563, 1666, 1673, 1777, 2298), `user_store.go` (471, 509, 622), `compression.go` (103), `config.go` (240), `middleware.go` (113), `remote_transport.go` (21)
- Modify test: `ratelimit_test.go` (33)
- Test: `internal/client/portable_backup_test.go`

**Interfaces:**
- Consumes: `extractPortableArchive(source io.Reader, destination string) error`.

- [ ] **Step 1: Write the guard test** `TestExtractPortableArchiveAcceptsLegacyRegularFileType`.
  - Build an in-memory tar.gz with one header: `Name: "uploads/legacy.txt"`, `Typeflag: '\x00'` (a literal, not `tar.TypeRegA`), `Size: 5`, `Mode: 0o644`, contents `hello`.
  - Extract it into `t.TempDir()` and assert `uploads/legacy.txt` reads `hello`.
- [ ] **Step 2: Run** `go test ./internal/client -run TestExtractPortableArchiveAcceptsLegacyRegularFileType`. Expected: PASS before the change.
- [ ] **Step 3: Drop the deprecated constant.** `case tar.TypeReg, tar.TypeRegA:` becomes `case tar.TypeReg:`, because the tar reader already converts `TypeRegA` to `TypeReg`. Re-run Step 2: still PASS.
- [ ] **Step 4: Fix errcheck.** Every listed `Close`/`RemoveAll`/`Remove`/`Rollback` becomes a `_ =` form. `portable_backup.go:473` becomes `defer func() { _, _ = conn.Exec(context.Background(), "ROLLBACK") }()`.
- [ ] **Step 5: Fix errorlint and bodyclose.**
  - `direct_media.go:129`: `!errors.Is(err, io.EOF)`.
  - `migration.go:456`: re-run `$LINT ./internal/client/...` after Step 4. If bodyclose still fires, close the body on every path that returns before the defer is reached.
- [ ] **Step 6: Fix staticcheck.**
  - `compression.go:103`: drop the embedded `ResponseWriter` from the selector.
  - `direct_media.go:53`: drop the redundant `error` type.
  - `middleware.go:113` and `remote_transport.go:21`: apply De Morgan's law.
  - `server.go:668` and `749`: tagged switches.
  - `config.go:240`: reword the message to start lowercase: `authentication is required: configure Discord sign-in, or set MEMEINDEX_ALLOW_ANONYMOUS=true only for intentional anonymous operation`. Grep `README.md` and the tests for the old text first.
  - `ratelimit_test.go:33`: split into two separate `rl.allow("k")` calls, each with its own `t.Fatal`.
- [ ] **Step 7: Delete `buildLoginURL`.** `grep -rn buildLoginURL internal` must then show nothing.
- [ ] **Step 8: Verify.** `$LINT ./internal/client/...` reports 0 issues, and `go test ./internal/client/...` passes.
- [ ] **Step 9: Commit:** "Fix client lint findings and drop deprecated tar.TypeRegA".

### Task 6: Verify PR 1 and open it

- [ ] **Step 1: Run everything.**
  - `$LINT ./...` reports `0 issues.`
  - `go build ./...`, `go vet ./...` and `go test ./...` pass.
  - `node --test scripts/*.test.cjs` passes.
- [ ] **Step 2: Push and open the PR.** Push `go-lint-backlog` and open a PR against `master` titled "Clear the Go lint backlog and gate CI on every finding". The body lists the five behavior-relevant fixes from the spec and the LF change. Bind it with the ccd_pr tools and read its CI.

---

## PR 2: split the three files (branch `go-file-splits`, from `go-lint-backlog`)

Every task here is a pure move. Each declaration keeps its doc comment and its exact text. Any declaration not listed stays in the original file. Imports are pruned per file until `go build ./...` passes.

### Task 7: `declcheck` and the accessor split

**Files:**
- Create (not committed): `$SCRATCH/declcheck/main.go`, plus a throwaway mover of your own design in `$SCRATCH`
- Create: `internal/accessor/postgres_memes.go`, `postgres_tags.go`, `postgres_shares.go`, `postgres_reel_sessions.go`, `postgres_audit.go`, `postgres_legacy_import.go`
- Modify: `internal/accessor/postgres_store.go`

**Interfaces:**
- Produces: `go run $SCRATCH/declcheck <package dir>` prints sorted `key<TAB>hash` lines. Tasks 8 and 9 use it the same way.

- [ ] **Step 1: Create the branch.** In the worktree, `git switch -c go-file-splits` from the tip of `go-lint-backlog`.
- [ ] **Step 2: Write `declcheck`.** It parses each non-test `.go` file in the directory, with CRLF normalized to LF, using `go/parser` and `ParseComments`. For every top-level declaration except imports, it prints:
  - the key: `func (<receiver type>).<Name>`, `func <Name>`, or `<type|var|const> <names joined by ,>`
  - a tab, then the SHA-256 of the source text from the doc comment's start (or the declaration's start) to its end.

  For every comment group that is neither a declaration's doc nor inside a declaration, it prints `comment<TAB><hash of its text>`. Output is sorted.
- [ ] **Step 3: Take baselines** for all three packages before moving anything: `go run $SCRATCH/declcheck internal/accessor > $SCRATCH/accessor.before`, and the same for `internal/manager` and `internal/client`.
- [ ] **Step 4: Move the declarations.**
  - `postgres_memes.go`: `List`, `AdminDashboardMemes`, `queryMemeRows`, `memeCounts`, `QueryMemes`, `MemeDashboard`, `GetByID`, `GetAnyByID`, `MemesByIDs`, `Random`, `Create`, `isUniqueViolation`, `duplicateError`, `Update`, `SetFavorite`, `SetFavoriteWithActor`, `HiddenMemeIDs`, `TotalFavoriteAssignments`, `FavoriteActivitySince`, `Delete`, `getByID`, `setFavoriteInExecutor`, `getByHash`, `scanMemeRow`, `CountFavoritesByUser`
  - `postgres_tags.go`: `TagCounts`, `TagUsageCounts`, `UntaggedWithoutSuggestionsCount`, `UntaggedWithoutSuggestionIDs`, `PendingSuggestionMemes`, `TagMergeCounts`, `MergeTag`, `SuggestTags`, `ListSuggestedTags`, `ReplaceSuggestedTags`, `SetSearchText`, `SetAutoSuggestDisabled`, `replaceTags`, `deleteOrphanTags`
  - `postgres_shares.go`: `GetOrCreateMemeShare`, `GetMemeShareState`, `ResolveMemeShareSlug`, `ListActiveMemeShares`, `RevokeMemeShare`, `RevokeAllMemeShares`
  - `postgres_reel_sessions.go`: `LoadReelSessions`, `SaveReelSession`, `DeleteReelSession`, `CleanupStaleReelSessions`
  - `postgres_audit.go`: `ListMemeAudit`, `ListAuditFeed`, `ListPendingDeletes`, `ApprovePendingDelete`, `RejectPendingDelete`, `RecordSystemAudit`, `insertAuditLog`, `diffAuditTags`, `CountAuditActionsByActor`
  - `postgres_legacy_import.go`: `importLegacyDataIfNeeded`, `backfillMissingContentHashes`, `importLegacyReelSessionsIfNeeded`, `readLegacyMemes`, `readLegacyFavorites`
  - Stays in `postgres_store.go`: the struct and constructor, `SearchIndexAvailable`, `UploadDir`, `Ping`, `Close`, `ThumbnailDir`, `EnsurePreviewAssets`, `videoMemesForPreview`, `ensureSchema`, `tableExists`, `queryable`, `AllStoredFiles`, `scanIDList`, `scanInt64List`, `countByUser`
- [ ] **Step 5: Verify.**
  - `go build ./...` and `gofmt -l internal/accessor` are clean.
  - `diff $SCRATCH/accessor.before <(go run $SCRATCH/declcheck internal/accessor)` prints nothing. If only `comment` lines differ, each must be a section banner made redundant by the new file; say which in the commit message.
  - `go test ./internal/accessor/...` passes.
- [ ] **Step 6: Commit:** "Split postgres_store.go by area (pure move)".

### Task 8: Split `meme_manager.go`

**Files:**
- Create: `internal/manager/meme_shares.go`, `tag_suggestions.go`, `tag_hygiene.go`, `admin_dashboard.go`, `moderation.go`
- Modify: `internal/manager/meme_manager.go`

**Interfaces:**
- Consumes: `declcheck` and `$SCRATCH/manager.before` from Task 7.

- [ ] **Step 1: Move the declarations.**
  - `meme_shares.go`: `GetOrCreateMemeShare`, `ResolveMemeShareLink`, `GetMemeShare`, `ActiveMemeShare`, `ListActiveMemeShares`, `ListActiveMemeSharesPage`, `memesByIDs`, `memeNamesByIDs`, `RevokeMemeShare`, `RevokeAllMemeShares`
  - `tag_suggestions.go`:
    - Types: `MemeTagSuggestionResult`, `TagSuggestionQueueStatus`, `QueuedTagSuggestionItem`, `PendingReviewMemeItem`, `ResetTagSuggestionQueueResult`, `tagSuggestionAttemptOptions`, `RetryFailedTagSuggestionsResult`
    - Methods and functions: `SuggestTags`, `GetStoredMemeTagSuggestions`, `RefreshMemeTagSuggestions`, `QueueMemeTagSuggestions`, `ResetTagSuggestionsAndRequeueUntagged`, `RetryFailedTagSuggestions`, `(*MemeManager).TagSuggestionQueueStatus`, `fillTagSuggestionCountsFromSQL`, `fillTagSuggestionCountsFromScan`, `StartTagSuggestionWorker`, `SeedTagSuggestionQueue`, `waitForOllama`, `retryOrDropTagSuggestion`, `unloadTagModelIfIdle`, `runTagSuggestionWorker`, `enqueueTagSuggestion`, `dequeueTagSuggestion`, `finishTagSuggestion`, `requeueTagSuggestion`, `setTagSuggestionWorkerState`, `recordTagSuggestionSuccess`, `DismissMemeTagSuggestion`, `DismissAllMemeTagSuggestions`, `ApplyMemeTagSuggestion`, `suggestionRequestForMeme`, `transcriptForVideo`, `suggestTagsForMeme`, `tagSuggestionAttemptsForMeme`, `shouldQueueTagSuggestionsForMeme`, `containsTag`, `removeTagValue`
  - `tag_hygiene.go`:
    - Types: `TagHygieneReport`, `TagHygieneTag`, `TagHygienePair`, `TagHygienePageParams`, `TagHygienePageResult`, `TagMergeResult`, `tagHygieneSnapshot`
    - Methods and functions: `(*MemeManager).TagHygieneReport`, `TagHygienePage`, `pageBounds`, `StartTagHygieneWorker`, `runTagHygieneWorker`, `refreshTagHygieneSnapshot`, `signalTagHygieneRefresh`, `markTagsChanged`, `computeTagHygieneReport`, `cloneTagHygieneReport`, `MergeTags`, `tagKeysLookLikeVariant`, `preferredCanonicalTag`, `compactTagKey`, `levenshteinDistance`
  - `admin_dashboard.go`: `AdminDashboardStats`, `AdminDashboardDayStat`, `AdminDashboardMetricStat`, `AdminDashboardRecentMeme`, `AdminDashboardTagStat`, `AdminDashboard`, `computeAdminDashboard`, `adminDashboardMemes`, `tagUsageCounts`, `invalidateAdminReadCache`, `cloneAdminDashboardStats`
  - `moderation.go`: `ListMemeAudit`, `ListAuditFeed`, `ListPendingDeletes`, `ApprovePendingDelete`, `RejectPendingDelete`, `IsMemeHidden`, `invalidateHiddenMemes`
- [ ] **Step 2: Verify** as in Task 7 Step 5, against `$SCRATCH/manager.before`, with `go test ./internal/manager/...`.
- [ ] **Step 3: Commit:** "Split meme_manager.go by area (pure move)".

### Task 9: Split `server.go`

**Files:**
- Create: `internal/client/server_access.go`, `app_shell.go`, `share_target.go`, `meme_handlers.go`, `link_import.go`, `admin_handlers.go`, `backup_handlers.go`, `user_handlers.go`, `auth_handlers.go`
- Modify: `internal/client/server.go`

**Interfaces:**
- Consumes: `declcheck` and `$SCRATCH/client.before` from Task 7.

- [ ] **Step 1: Move the declarations.**
  - `server_access.go`: `writeSessionUnavailable`, `writeSessionUnavailablePage`, `withPageAuth`, `withProtectedAssetAuth`, `allowRevalidatedBrowserCache`, `assetMemeID`, `noDirectoryListing`, `setUntrustedAssetHeaders`, `scriptableContentType`, `setProtectedAssetHeaders`, `allowsAnonymousLinkPreview`, `rewriteAssetPathsInResponses`, `protectAssetPathForResponse`, `protectMemeForResponse`, `protectMemesForResponse`, `protectMemeListForResponse`, `protectPendingDeletesForResponse`, `protectAuditFeedForResponse`, `allowsAuthenticatedShellOnly`, `withAPIAuth`, `hasPermission`, `requirePermission`
  - `app_shell.go`: `handleWebManifest`, `handleServiceWorker`, `staticAssetHandler`, `indexShellState`, `renderIndexShell`, `handleIndex`, `handleAccessDeniedPage`, `buildAssetURL`, `assetFingerprint`, `assetContentCache`, `assetContentHash`, `handleOGImage`, `isLinkPreviewRequest`
  - `share_target.go`: `handleShareTarget`, `sharedURLPattern`, `firstSharedURL`, `sharedNotes`, `capSharedNote`
  - `meme_handlers.go`: `handleMemes`, `handleMemeByID`, `getStoredMemeTagSuggestions`, `refreshMemeTagSuggestions`, `updateStoredMemeTagSuggestions`, `listMemes`, `handleDashboard`, `createMeme`, `memePatchPayload`, `resolveMemePatch`, `updateMeme`, `updateFavorite`, `deleteMeme`, `handleTags`, `handlePopularTags`, `handleRandomMeme`, `handleReelSession`
  - `link_import.go`: `handleSaveLink`, `safeLinkFilename`, `errUnsupportedMediaURL`, `errUndownloadableSourceURL`, `createMemeFromSourceURL`, `shouldQueueLinkRetry`, `processRetriedLinkJob`, `friendlySourceDownloadError`, `normalizeSourceURL`, `validateRemoteURL`, `sourceURLCandidates`
  - `admin_handlers.go`: `adminSystemHealth`, `adminDashboardResponse`, `handlePendingDeleteQueue`, `handleAdminDashboard`, `handleAuditLogs`, `handleTagHygiene`, `handleTagSuggestionStatus`, `handleResetTagSuggestions`, `handleRetryFailedTagSuggestions`, `handleLinkDownloadStatus`, `handleLinkDownloadActions`, `handleAdminMemeActions`, `handleAdminMemeByID`, `handleMemeAudit`
  - `backup_handlers.go`: `handleBackupExport`, `handleBackupStatus`, `sanitizeDownloadFilename`, `handleBackupDownload`, `handleBackupImport`, `runBackupRestore`, `handleBackupImportParts`, `handleBackupImportManifest`, `handleBackupImportPartsRestore`
  - `user_handlers.go`: `handleUsers`, `handleUserByID`, `listUsers`, `createManagedUser`, `updateManagedUser`, `deleteManagedUser`, `managedUserPayload`, `normalizePermissionTags`
  - `auth_handlers.go`: `handleLogin`, `handleOAuthCallback`, `handleLogout`, `handleAuthSession`
  - Stays in `server.go`: `Server`, `NewServer`, `Routes`, `maintenanceGuard`, `isMutatingMethod`, `currentAuditActor`, `diffTags`, `contextWithSession`, `currentUserID`, `jsonResponseBufferPool`, `maxPooledJSONBuffer`, `writeJSON`, `splitTags`, `parseQueryInt`
- [ ] **Step 2: Verify** as in Task 7 Step 5, against `$SCRATCH/client.before`, with `go test ./internal/client/...`.
- [ ] **Step 3: Commit:** "Split server.go by area (pure move)".

### Task 10: Verify PR 2 and open it

- [ ] **Step 1: Run everything.** `$LINT ./...` reports `0 issues.`, and `go build ./...`, `go vet ./...` and `go test ./...` pass. All three `declcheck` diffs are empty, apart from comment lines already explained in the commits.
- [ ] **Step 2: Push and open the PR.** Push `go-file-splits` and open a PR titled "Split server.go, meme_manager.go and postgres_store.go by area". Target `go-lint-backlog` until PR 1 merges, then `master`. The body states that it is a pure move and how `declcheck` proved it. Bind it with the ccd_pr tools.

## After PR 1 merges

The main checkout's Go files stay CRLF until rewritten. Once nothing Go-related is modified there, repeat Task 1 Step 4 in the main checkout.
