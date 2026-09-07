# MemeIndex security review — 2026-09-07

Scope: local source review and low-volume, anonymous HTTPS checks against memes.cooplabs.net using the supplied share. No production uploads, account changes, revocations, brute force, load testing, or requests to internal network targets were performed. Local source is not proof that every reviewed code path is deployed. No production patch has been deployed.

The [second-pass review](SECURITY_REVIEW_SECOND_PASS.md) supersedes the outstanding-work and validation sections below with additional findings and fixes.

## Findings patched locally

| Priority | Finding | Evidence | Local fix |
| --- | --- | --- | --- |
| High | Stored cross-site scripting in file previews | File names and supplied Content-Type values reach innerHTML without escaping. The upload store preserves those values. An uploader could inject markup that runs in a viewer's or administrator's origin. Confirmed by source tracing; no malicious production upload attempted. | Escape metadata in card, modal, reel, and upload fallback previews. Regression tests exercise hostile metadata in all four renderers. |
| Medium | OAuth state is not bound to the initiating browser | Production accepted a freshly issued state with no cookie and proceeded to the missing-code check (`400 missing oauth code`). Code accepted any pending state and also permitted signed-cookie replay after consumption or expiration. This enables login CSRF/session confusion, not proof of administrator account takeover. | Require a matching signed browser cookie and an unexpired, single-use pending state. Invalid callbacks cannot consume another browser's state. |
| Medium | Forwarded-host poisoning in shared URLs | A production request with `X-Forwarded-Host: security-test.invalid` caused that host to appear in the shared page. This lets request headers influence canonical/media metadata containing the bearer share token. A practical victim-delivery or cache-poisoning chain was not demonstrated. | Build share origins from Host instead of the unchecked forwarded-host header. |
| Medium | Shared media allows caching beyond revocation or expiry | Production media returned `Cache-Control: public, max-age=300` and the equivalent CDN directive. A compliant cache may serve already-fetched bytes for five minutes without rechecking share state. The sampled Cloudflare response was DYNAMIC; a Cloudflare cache hit after revocation was not demonstrated. | Set no-store for all share routes and media, including CDN-specific headers and redirects. Existing origin-side revocation tests remain passing. |

## Production checks that held

- Anonymous requests to `/api/memes`, `/api/users`, and `/api/admin/backup/download` returned 401.
- Anonymous `/uploads/` returned 404 and private/no-store headers.
- The supplied meme page without a token redirected to login.
- A malformed share signature redirected to the authenticated path instead of serving media.
- The valid shared page had restrictive CSP, no-referrer, no-store, and noindex directives.
- The OAuth state cookie had HttpOnly, Secure, and SameSite=Lax.
- Spoofing X-Forwarded-Host did not change the configured Discord callback URL in the sampled login response.

These are sampled checks, not a guarantee that all authorization paths are safe. No authenticated role matrix was exercised in production.

## Follow-up risks at the end of the first pass

1. **Authentication configuration fails open.** `DiscordAuthConfig.Enabled` requires every setting; missing one causes the auth service to be nil and middleware to pass requests through. Production authentication is currently enabled. Add explicit opt-in for local anonymous operation and reject incomplete production auth configuration at startup.
2. **Outbound fetching needs stronger SSRF isolation.** `normalizeSourceURL` validates DNS before a separate HTTP connection, leaving a DNS rebinding gap. It also returns the original URL on HTTP errors, including rejected redirects, before passing candidates into mediafetch/yt-dlp. Internal-address protection must apply to every actual connection, including downstream tools and redirects. Add connection-time IP enforcement and restricted downloader egress; test with a controlled local fixture. No production internal-network probing was performed.
3. **Uploads lack an application-level total request size limit.** `ParseMultipartForm(256 << 20)` is a memory threshold, not a total upload cap. An authorized uploader can consume disk/resources. Add a configured MaxBytesReader limit and appropriate downloader quotas/timeouts. Proxy limits may reduce exposure but were not verified.
4. **Origin and proxy trust need explicit configuration.** The patch assumes the reverse proxy preserves the public Host. Dynamic OAuth redirects and scheme/cookie handling still consult forwarding headers. Prefer a configured public origin and ensure only trusted proxies can reach the origin and overwrite forwarded headers.
5. **Rate/resource controls need an infrastructure review.** Login creates in-memory pending states; the application HTTP server has no explicit header/idle timeouts. Verify edge rate limits and origin exposure, then add bounded state storage and server timeouts. No load tests were performed.

## Validation and rollout

- `go test ./...` passed.
- `node --test scripts/*.test.cjs` passed: 32 tests.
- `git diff --check` passed.
- OAuth regression covers absent/mismatched cookies, legitimate completion, replay, and expiration.
- Share regression covers HTML, media, and preview responses with a spoofed forwarded host and checks all cache-control layers.
- Frontend regression executes the affected renderers with hostile metadata and verifies escaped output; it is not a full browser end-to-end exploit test.

Deploy the backend and frontend together. Verify the reverse proxy preserves `Host: memes.cooplabs.net`. With the stricter OAuth state check, login attempts started before a restart must be restarted; multiple application replicas require sticky routing or a shared single-use state store. Shared media will use more bandwidth because it cannot be cached. Purge any existing edge-cached share media when deploying; already downloaded copies or third-party preview caches cannot be recalled. Retest the anonymous checks and complete a normal Discord login after deployment.

The supplied share token is intentionally omitted from this report because it grants access to the meme.
