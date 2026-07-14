# MemeIndex

MemeIndex is a self-hosted meme organizer built with Go and a lightweight frontend. It is designed for messy real-world meme folders where assets can be images, videos, audio, archives, PSDs, or whatever else needs to stay searchable.

## Backend structure

- `internal/client`: HTTP-facing layer, routing, config, and middleware
- `internal/manager`: application orchestration and normalization
- `internal/accessor`: file persistence and metadata storage

## Features

- Upload any file type through the browser
- Download supported YouTube, Facebook, and Reddit links from a dedicated Process Link modal
- Store metadata in Postgres with a dedicated tags table for suggestions
- Compute content hashes on upload so duplicate files can be skipped
- Generate video thumbnails for lighter grid previews when `ffmpeg` is installed
- Suggest reviewable tags for images and videos through an optional local Ollama model
- Keep the original source link attached to imported link-based memes
- Preview image and video files inline
- Search by filename, notes, and tags
- Mark favorites for quick filtering
- Keep favorites per Discord user
- Update notes and tags after upload
- Delete files from the archive

## Run locally

```powershell
$env:GOTELEMETRY='off'
go run .
```

Then open `http://localhost:8080`.

## Run with `.env`

This repo now includes a `Taskfile.yml` so you can keep local settings in a `.env` file and inject them automatically at runtime.

1. Install `task` if you do not already have it: https://taskfile.dev
2. Copy `.env.example` to `.env`
3. Fill in your real Discord values and allowed user IDs
4. Run:

```powershell
task run
```

To build with the same env file loaded:

```powershell
task build
```

The real `.env` file is ignored by git, so secrets stay local.

## Bulk Upload Script

This repo includes a PowerShell uploader at [`scripts/upload-memes.ps1`](/f:/GitHub/MemeIndex/scripts/upload-memes.ps1) for importing a large meme folder through the normal authenticated upload API.

Example with the raw auth token:

```powershell
.\scripts\upload-memes.ps1 `
  -FolderPath "F:\Memes\Backlog" `
  -BaseUrl "https://memes.cooplabs.net" `
  -SessionToken "PASTE_THE_RAW_TOKEN_HERE" `
  -Recurse `
  -BatchSize 10
```

Example if you copied the exact `memeindex_session` cookie value from the browser instead:

```powershell
.\scripts\upload-memes.ps1 `
  -FolderPath "F:\Memes\Backlog" `
  -BaseUrl "https://memes.cooplabs.net" `
  -CookieValue "PASTE_THE_COOKIE_VALUE_HERE" `
  -Recurse
```

Optional flags:

- `-Tags "tag1,tag2"` applies the same comma-separated tags to every uploaded file in the batch
- `-Notes "Imported backlog"` applies the same notes field to every uploaded file in the batch
- `-DryRun` prints the files that would be uploaded without sending anything

## Configuration

- `MEMEINDEX_ADDR`: server bind address, default `:8080`
- `MEMEINDEX_DATA_DIR`: data directory, default `data`
- `MEMEINDEX_DATABASE_URL`: Postgres connection string. When empty, MemeIndex falls back to the legacy JSON store
- `MEMEINDEX_MEDIAFETCH_YTDLP_BINARY`: path to the `yt-dlp` binary used for link downloads, default `yt-dlp`
- `MEMEINDEX_MEDIAFETCH_RETRY_INTERVAL_SECONDS`: how long failed link imports wait before retrying, default `300`
- `MEMEINDEX_MEDIAFETCH_RETRY_MAX_ATTEMPTS`: how many retry attempts failed link imports get before moving to the rejected queue, default `3`
- `MEMEINDEX_TAGSUGGEST_OLLAMA_URL`: optional Ollama base URL, for example `http://ollama:11434`
- `MEMEINDEX_TAGSUGGEST_OLLAMA_MODEL`: optional Ollama vision model name, default compose value `qwen2.5vl:3b`
- `MEMEINDEX_TAGSUGGEST_TIMEOUT_SECONDS`: request timeout for tag suggestions, default `300`
- `MEMEINDEX_TAGSUGGEST_MAX_TAGS`: max number of suggested tags to return, default `8`
- `MEMEINDEX_TAGSUGGEST_KNOWN_TAG_BUDGET`: how many frequently used archive tags to send as optional reuse hints, default `60` and capped at `60` in the model prompt
- `MEMEINDEX_TAGSUGGEST_FAST_MODE`: when `true`, uses a cheaper video path tuned for slower hardware by defaulting to 1 smaller frame and disabling transcription
- `MEMEINDEX_TAGSUGGEST_GENERATE_ONLY`: when `true`, use Ollama's `/api/generate` endpoint with the same JSON schema instead of `/api/chat`. This is useful for models that are more stable on the generate endpoint
- `MEMEINDEX_TAGSUGGEST_VIDEO_FRAME_COUNT`: override how many video frames are sampled for tag suggestions. Defaults to `3`, or `1` in fast mode
- `MEMEINDEX_TAGSUGGEST_VIDEO_FRAME_WIDTH`: override the per-frame resize width for video tag suggestions. Defaults to `480`, or `320` in fast mode
- `MEMEINDEX_TAGSUGGEST_DISABLE_TRANSCRIPTION`: when `true`, skip optional audio transcription even if a transcription command is configured
- `MEMEINDEX_TAGSUGGEST_TRANSCRIBE_BINARY`: optional local speech-to-text command for video audio; when unset, video suggestions stay frame-only
- `MEMEINDEX_TAGSUGGEST_TRANSCRIBE_ARGS`: optional comma-separated args for that command. Use `{input}` where the extracted WAV path should be inserted; if omitted, MemeIndex appends the WAV path automatically
- `MEMEINDEX_TAGSUGGEST_TRANSCRIBE_TIMEOUT_SECONDS`: timeout for the optional transcription command, default `120`
- `MEMEINDEX_DISCORD_CLIENT_ID`: Discord OAuth application client ID
- `MEMEINDEX_DISCORD_CLIENT_SECRET`: Discord OAuth application client secret
- `MEMEINDEX_DISCORD_REDIRECT_URL`: Discord OAuth callback URL, for example `http://localhost:8080/auth/callback`
- `MEMEINDEX_DISCORD_DYNAMIC_REDIRECT`: when `true`, dev mode builds the callback URL from the current browser host, so `localhost` and your LAN IP can both work
- `MEMEINDEX_SESSION_SECRET`: random secret used to sign auth cookies
- `MEMEINDEX_SESSION_DURATION_DAYS`: how long Discord login cookies stay valid, default `30`
- `MEMEINDEX_COOKIE_SECURE`: set to `true` when serving over HTTPS so auth cookies are marked secure
- `MEMEINDEX_SUPER_ADMIN_USER_IDS`: comma-separated Discord user IDs that should always have full access plus user-management access
- `MEMEINDEX_VIEW_USER_IDS`: comma-separated Discord user IDs allowed to view the app
- `MEMEINDEX_ADD_USER_IDS`: comma-separated Discord user IDs allowed to view and upload memes

If the Discord OAuth env vars are not set, MemeIndex keeps auth disabled and behaves like it does today.

If the Ollama env vars are not set or the model is offline, MemeIndex still works normally and only the suggested-tags button becomes unavailable.

- `VIEW`: browse memes, tags, uploads, and random reel
- `UPLOAD`: everything in `VIEW`, plus upload new memes
- `ADD TAGS`: add tags to existing memes
- `REMOVE TAGS`: remove tags from existing memes
- `DELETE`: delete memes
- `SUPER ADMIN`: full access plus the users modal, where all non-super-admin permissions can be toggled and stored in Postgres

When Postgres is enabled, MemeIndex imports any missing IDs from `VIEW_USER_IDS` and `ADD_USER_IDS` into the users table on startup. Super admins come from env, while all other user permissions are stored in the database and recalculated on every request without rotating auth cookies.

Example local setup:

```powershell
$env:MEMEINDEX_DATABASE_URL="postgres://memeindex:memeindex@localhost:5432/memeindex?sslmode=disable"
$env:MEMEINDEX_DISCORD_CLIENT_ID="123456789012345678"
$env:MEMEINDEX_DISCORD_CLIENT_SECRET="your-discord-client-secret"
$env:MEMEINDEX_DISCORD_REDIRECT_URL="http://localhost:8080/auth/callback"
$env:MEMEINDEX_DISCORD_DYNAMIC_REDIRECT="false"
$env:MEMEINDEX_SESSION_SECRET="replace-this-with-a-long-random-string"
$env:MEMEINDEX_SESSION_DURATION_DAYS="30"
$env:MEMEINDEX_SUPER_ADMIN_USER_IDS="333333333333333333"
$env:MEMEINDEX_VIEW_USER_IDS="111111111111111111,222222222222222222"
$env:MEMEINDEX_ADD_USER_IDS="222222222222222222"
```

For local development across both your PC and phone, you can instead enable dynamic redirects:

```powershell
$env:MEMEINDEX_DISCORD_DYNAMIC_REDIRECT="true"
```

Then add both callback URLs in the Discord developer portal, for example:

- `http://localhost:8080/auth/callback`
- `http://192.168.1.123:8080/auth/callback`

When enabled, MemeIndex will use whichever host you started from in the browser.

## Docker Compose

This repo now includes a `docker-compose.yml` that starts:

- `app`: MemeIndex
- `postgres`: Postgres for meme metadata, favorites, and tag suggestions
- `ollama`: optional local LLM runtime for tag suggestions
- `ollama-model`: one-shot helper that pulls the configured local vision model into the shared Ollama volume
- `cloudflared`: optional Cloudflare Tunnel sidecar for exposing only the app

Bring it up with:

```powershell
docker compose pull
docker compose up -d
```

### Move or back up a MemeIndex server

Sign in as a super admin, open **Admin > Backup & Restore**, and select **Create Backup**. The export runs as a shared server-side task, so you can leave the page while it works and any super admin can monitor its status. When complete, MemeIndex keeps the `.tar.gz` archive available under **Download Latest** until a newer backup completes. The archive contains:

- all original meme files and generated thumbnails
- all meme metadata and tags
- per-user favorites and permissions
- reel sessions, moderation state, and audit history

On the destination Docker instance, open the same admin page and import that archive. Import replaces the destination library and application database, so export the destination first if it contains anything you may need. The destination should run the same or a newer MemeIndex version so its database schema supports every field in the backup.

The browser workflow requires PostgreSQL storage (the default Docker Compose configuration). Environment secrets, Discord OAuth credentials, session signing keys, the Ollama model volume, and other `.env` settings are intentionally not included; copy those deployment settings separately.

### Install on a phone

MemeIndex is an installable Progressive Web App when served over HTTPS. Open the account menu and select **Install > Add to Home Screen**.

- On Android and supported desktop browsers, MemeIndex opens the browser's native installation prompt.
- On iPhone or iPad, open MemeIndex in Safari, tap **Share**, select **Add to Home Screen**, and enable **Open as Web App** when that option is shown.

The installed app gets its own home-screen icon and standalone window. Its service worker caches only the public manifest and app icons; API responses, account details, thumbnails, and meme files remain network-only and are never placed in the offline cache.

Or with Task:

```powershell
task docker-up
```

The app is exposed on `http://localhost:8080`.

To also start the optional Cloudflare Tunnel service:

```powershell
task docker-tunnel-up
```

Or directly:

```powershell
docker compose --profile tunnel pull
docker compose --profile tunnel up -d
```

Notes:

- uploaded files still live on disk under `./data/uploads`
- link downloads are staged under `./data/downloads` and cleaned up after import
- Postgres keeps metadata, favorites, and the dedicated tags table
- suggested tags come from the original image file for stills, or several sampled video frames for videos
- if you configure a local transcription command, video audio is extracted to WAV and its transcript is included as extra meme context for tagging
- the default model is `qwen2.5vl:3b`, which is a smaller vision model and usually a better fit for reading meme text than the earlier default
- the compose file keeps Ollama models warm with `OLLAMA_KEEP_ALIVE=30m` by default so back-to-back suggestions do not need a fresh cold load every time
- if Ollama is down, still starting, or missing the configured model, the app stays online and tag suggestions simply return unavailable
- Postgres also stores random reel sessions when `MEMEINDEX_DATABASE_URL` is enabled
- on first Postgres startup, if the database is empty and legacy `data/index.json` or `data/favorites.json` files exist, MemeIndex imports them automatically
- stale reel sessions are cleaned by the app every night at `00:00 UTC`
- Postgres is not exposed by the compose file, so the app remains the only service talking to the database

## Local Container Development

If you want to test changes in a local container instead of deploying first, this repo now includes a [`docker-compose.dev.yml`](/f:/GitHub/MemeIndex/docker-compose.dev.yml) override that builds the app from your local source tree.

Run it with:

```powershell
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

Or with Task:

```powershell
task docker-dev-up
```

That keeps Postgres in Docker Compose, but swaps the app service from the published GHCR image to a local image build tagged `memeindex:dev`.

Notes:

- the local dev container defaults to `http://localhost:8081` so it can coexist with anything already using `8080`
- the shared compose file now uses `MEMEINDEX_HOST_PORT` for host port binding, defaulting to `8080`
- if you run compose directly, you can choose a different host port with `MEMEINDEX_HOST_PORT`, for example `MEMEINDEX_HOST_PORT=8090`
- this is a rebuild loop, not hot reload, so after code changes you should rerun `task docker-dev-up`
- for the fastest inner loop while editing Go or frontend files, `task run` is still quicker than rebuilding the container each time
- to stop the local dev stack, run `task docker-dev-down`
- the app env vars are declared directly in `docker-compose.yml`, and Docker Compose fills them from your shell or local `.env`
- `MEMEINDEX_IMAGE` controls which published app image Compose pulls, and defaults to `ghcr.io/your-github-user-or-org/memeindex:latest`
- if `ffmpeg` is available in the container or host environment, MemeIndex generates JPEG thumbnails for videos and backfills thumbnails for older imported videos in the background on startup
- the app container now includes `ffmpeg` plus the current upstream `yt-dlp` Linux binary, which the upload modal uses for supported link downloads

To verify the downloader inside the local app image:

```powershell
task docker-dev-verify-mediafetch
```

## GitHub Container Publishing

This repo includes [`.github/workflows/docker-publish.yml`](/f:/GitHub/MemeIndex/.github/workflows/docker-publish.yml), which builds the app image in GitHub Actions and pushes it to GitHub Container Registry on:

- pushes to `main`
- version tags like `v1.0.0`
- manual runs from the Actions tab

The published image path is:

```text
ghcr.io/<owner>/<repo>
```

For this repository, that usually means a package name like `ghcr.io/<your-github-user-or-org>/memeindex`.

### One-time setup

1. Push this repository to GitHub.
2. Update `.env` so `MEMEINDEX_IMAGE` points at your real package, for example `ghcr.io/example-org/memeindex:latest`.
3. Run the workflow once by pushing to `main` or using `workflow_dispatch`.
4. In GitHub, make the container package public if you want hosts to pull it without logging in.

If you prefer to keep the package private, log in on the deployment host first:

```powershell
echo $env:GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

Then Compose can pull the image normally.

### Cloudflare Tunnel

The optional `cloudflared` service is meant to expose only MemeIndex, not Postgres.

Set these before starting the tunnel profile:

- `CLOUDFLARE_TUNNEL_TOKEN`: your Cloudflare tunnel token
- `MEMEINDEX_DISCORD_REDIRECT_URL`: your public HTTPS callback URL, for example `https://memes.example.com/auth/callback`
- `MEMEINDEX_COOKIE_SECURE=true`

Recommended behavior behind Cloudflare Tunnel:

- keep `MEMEINDEX_DISCORD_DYNAMIC_REDIRECT=false` in production
- use a fixed public callback URL in Discord's developer portal
- keep Postgres internal with no published `5432` port

If you only want Cloudflare access and do not want local host exposure, remove or override the app's `8080:8080` port mapping in Compose for your deployment.

## GitHub Prep

For publishing this repo safely:

- commit `.env.example`
- do not commit `.env`
- do not commit `data/`

The included `.gitignore` is set up for that flow already.

## Storage layout

When using Postgres:

- `data/uploads/`: stored files
- Postgres `memes`: metadata rows
- Postgres `tags`: normalized tag catalog for suggestions
- Postgres `meme_tags`: meme-to-tag links
- Postgres `user_favorites`: per-user favorites
- Postgres `reel_sessions`: random reel history and position state

When using the legacy file store:

- `data/index.json`: metadata catalog
- `data/favorites.json`: per-user favorite meme IDs
- `data/uploads/`: stored files

## Next ideas

- Folder import and duplicate detection
- Drag-and-drop uploads
- Thumbnail generation for videos and documents
- Discord guild and role-based authorization
