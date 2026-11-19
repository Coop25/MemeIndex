# Go code health: lint backlog and file splits

Date: 2026-11-13. Size: a small change in design terms (no new behavior), delivered as two PRs.

## Why

The Go side carries a lint backlog that CI only ignores (`new-from-merge-base` in `.golangci.yml`), and three files have grown past the point where they read well: `internal/client/server.go` (3,249 lines), `internal/manager/meme_manager.go` (2,651) and `internal/accessor/postgres_store.go` (2,445). This is the first step of the code-health work; browser smoke tests and the `static/app.js` split come after, each designed separately.

## Goals

- Running `gofmt` or `golangci-lint` locally on Windows gives the same answer as CI.
- The lint backlog is zero, and CI gates on every finding, not only new ones.
- The three big files are split by area into files in the same package, so each area can be read on its own.

## Non-goals

- No behavior changes beyond what a lint fix requires (an error that is now checked or wrapped).
- No new packages, no renamed or newly exported identifiers, no API or schema changes.
- Nothing on the frontend.

## Anti-goals

- A "pure move" PR that also changes code. If a reviewer cannot trust that the split moved code without touching it, it has failed even if tests pass.
- Silencing real findings through config exclusions.

## What we found

- **Line endings.** Git stores Go files with LF, but this Windows checkout converts them to CRLF, so local `gofmt` flags every Go file (87 of the 165 local lint findings). CI on Linux never sees these.
- **The real backlog is 78 findings:** errcheck 34, staticcheck 28, errorlint 9, misspell 4, unused 2, bodyclose 1.
- Most are benign (deferred `Close` on readers and response bodies, `tx.Rollback` after commit, style quick-fixes in tests). A few matter:
  - `accessor/meme_store.go:465`: the upload file's `Close` is deferred and unchecked, so a failed final write would still produce a meme pointing at a short file.
  - `accessor/postgres_store.go:1213`: a type assertion on the pgx error decides whether a concurrent duplicate upload is reported as a duplicate; it misses wrapped errors (should be `errors.As`).
  - `client/direct_media.go:129`: an error compared with `!=` (should be `errors.Is`).
  - `tagsuggest/service.go` (6) and `manager/meme_manager.go` (1): `fmt.Errorf` with `%v`, which drops the wrapped error.
  - `client/portable_backup.go:1158`: deprecated `tar.TypeRegA`.
  - `client/auth.go` `buildLoginURL` and `manager/meme_manager.go` `looksLikeTagVariant` are unused.

## Approaches

**Line endings.** (a) Add `*.go`, `go.mod` and `go.sum` with `text eol=lf` to `.gitattributes`, or (b) tell each developer to set `core.autocrlf`. Picked (a): it lives in the repo and already has precedent there for shell scripts.

**Lint findings.** (a) Fix each finding in the code, or (b) add errcheck `exclude-functions` for `Close`/`Rollback`. Picked (a): (b) would also hide the upload `Close` that matters. Deliberately ignored errors become an explicit `_ =` (or a small deferred closure), real ones get a real check.

**File splits.** (a) More files in the same package, or (b) new sub-packages. Picked (a): (b) would force exporting internals and change call sites, which breaks the pure-move rule.

## Design

**PR 1: line endings and lint backlog.**
- `.gitattributes` gains LF rules for Go files; the working copies are rewritten to LF (no change in git's view, since the index is already LF).
- Fix all 78 findings as above. Each behavior-relevant fix (the five bullets above) comes with a test where one is practical.
- Remove `new-from-merge-base` from `.golangci.yml` so CI gates on every finding.
- Also carries the already-made README "Next ideas" refresh.

**PR 2: split the three files, pure moves only.**
- `server.go` keeps the server struct, constructor, routes and shared helpers; handlers move out by area (meme handlers, link import, admin and backups, users, auth, app shell and static assets, share target).
- `meme_manager.go` keeps the manager struct, constructors, lifecycle and core CRUD; shares, tag suggestions, tag hygiene and merges, admin dashboard, and moderation (audit, pending deletes, hidden memes) move to their own files.
- `postgres_store.go` keeps the store struct, connection, schema and legacy import; memes, tags and suggestions, shares, reel sessions, and audit and pending deletes move to their own files.
- Verified mechanically: the set of top-level declarations in each package, and their bodies, are identical before and after (only the file they live in changes).

## Calls I made (check these)

- Two PRs, lint first, so the move PR moves already-clean code.
- CI switches to gating on every lint finding once the backlog is zero.
- The README refresh rides along in PR 1.
- The exact file names and groupings in PR 2 are left to the plan, following the areas above.

## Left to the builder

- Exact file names for PR 2, matching the existing naming in each package.
- Whether a particular benign finding is best written as `_ =` or a deferred closure.
- Test shape for each behavior-relevant fix.
