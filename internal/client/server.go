package client

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	mediafetch "github.com/Coop25/mediafetch-go"
	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

type Server struct {
	config              Config
	managers            *manager.MemeManager
	auth                *authService
	users               authUserStore
	audiobooks          audiobookStore
	audiobookAdminStats audiobookAdminStatsCache
	audiobookActivity   audiobookActivityCache
	mediaClient         *mediafetch.Client
	linkRetries         *linkRetryQueue
	// background tracks goroutines that outlive a request (detached link
	// imports, the link retry queue) so StopBackgroundWork can wait for them
	// before main closes the store.
	background  backgroundTasks
	backup      *portableBackup
	shareSecret []byte
	draining    atomic.Bool
	// restoring is set for the full duration of a portable-backup import, which
	// truncates the database and swaps the uploads/thumbnails directories under
	// the running server. While it is set, maintenanceGuard rejects mutating
	// requests with 503 so nothing reads or writes half-restored state.
	restoring atomic.Bool
	// migrationTokens and migration are the two halves of server-to-server
	// migration (migration.go): tokens this server hands out as a source, and
	// the job pulling another server's library in as a destination.
	migrationTokens migrationTokenStore
	migration       migrationImporter
	// audiobookRemuxMu runs MP3 -> M4B audiobook remuxes one at a time (see
	// remuxAudiobookMP3).
	audiobookRemuxMu sync.Mutex
	// audiobookRemuxProgress feeds the admin Audiobook Conversion tab
	// (audiobook_remux_status.go).
	audiobookRemuxProgress *audiobookRemuxProgress
	// audiobookSeriesLookup overrides the catalog used for series suggestions
	// (audiobook_series_lookup.go); nil means the shared Audible lookup.
	audiobookSeriesLookup *cachedSeriesLookup
	// storage caches the admin storage report and holds the last orphan scan,
	// the allow-list for orphan deletes (admin_storage.go).
	storage adminStorageState

	authLimiter       *rateLimiter
	writeLimiter      *rateLimiter
	linkImportLimiter *rateLimiter
}

func NewServer(config Config, memeManager *manager.MemeManager) *Server {
	if err := config.validateSecurity(); err != nil {
		log.Fatalf("unsafe configuration: %v", err)
	}
	shareSecret, err := loadOrCreateShareSecret(config.DataDir, config.ShareSecret)
	if err != nil {
		log.Fatalf("share secret init failed: %v", err)
	}
	userStore, err := newAuthUserStore(context.Background(), config.DatabaseURL)
	if err != nil {
		log.Fatalf("auth user store init failed: %v", err)
	}
	if userStore != nil {
		if err := userStore.BootstrapFromConfig(context.Background(), config.DiscordAuth); err != nil {
			log.Fatalf("auth user bootstrap failed: %v", err)
		}
	}
	audiobookStore, err := newAudiobookStore(context.Background(), config.DatabaseURL, config.DataDir)
	if err != nil {
		log.Fatalf("audiobook store init failed: %v", err)
	}

	// Keep downloader egress on the pinned public-only transport. With no proxy
	// configured this blocks private/reserved addresses at connection time even
	// for the mediafetch dependency's un-injectable http.DefaultClient calls;
	// with a proxy configured that forward proxy is the egress boundary.
	pinDownloaderDefaultTransport(config.MediaFetchProxy)

	mediaClient, err := mediafetch.NewClient(mediafetch.ClientConfig{
		DownloadDir: filepath.Join(config.DataDir, "downloads"),
		YTDLPBinary: config.MediaFetchYTDLPBinary,
		YTDLPSettings: mediafetch.YTDLPSettings{
			Proxy: config.MediaFetchProxy,
		},
	})
	if err != nil {
		log.Fatalf("mediafetch client init failed: %v", err)
	}

	server := &Server{
		config:      config,
		managers:    memeManager,
		auth:        newAuthService(config.DiscordAuth, userStore),
		users:       userStore,
		audiobooks:  audiobookStore,
		mediaClient: mediaClient,
		backup:      newPortableBackup(config.DatabaseURL, config.DataDir),
		shareSecret: shareSecret,

		audiobookRemuxProgress: newAudiobookRemuxProgress(),
	}
	server.linkRetries = newLinkRetryQueue(config.MediaFetchRetry.Interval, config.MediaFetchRetry.MaxAttempts, server.processRetriedLinkJob)
	// Keep queued and rejected link imports across restarts and deploys. A
	// queue file that cannot be read is not fatal: the server starts with an
	// empty in-memory queue and leaves the file alone for inspection.
	if err := server.linkRetries.persistTo(filepath.Join(config.DataDir, "link_retries.json")); err != nil {
		log.Printf("link retry queue: could not load saved queue, starting empty and not saving: %v", err)
	}
	server.background.Go(func(ctx context.Context) {
		server.linkRetries.run(ctx, server.background.Quit())
	})
	server.startAudiobookUploadCleanupSweeper()
	server.startAudiobookBookmarkExpirySweeper()
	go server.backfillAudiobookMetadata()

	// Seed the metrics that are known at startup: the running build, and (for a
	// Postgres deployment) whether the trigram search indexes came up.
	SetMetricsBuildVersion(BuildVersion())
	if reporter, ok := server.storeForHealth().(searchIndexReporter); ok {
		metrics.setGauge("memeindex_search_trgm_index_available", boolGauge(reporter.SearchIndexAvailable()))
	}

	if config.RateLimitEnabled {
		server.authLimiter = newRateLimiter(authRatePerSecond, authRateBurst)
		server.writeLimiter = newRateLimiter(writeRatePerSecond, writeRateBurst)
		server.linkImportLimiter = newRateLimiter(linkImportRatePerSecond, linkImportRateBurst)
		server.startRateLimiterSweeper()
	} else {
		log.Printf("MemeIndex rate limiting: disabled")
	}

	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.Handle("/auth/login", s.rateLimitByIP(http.HandlerFunc(s.handleLogin), s.authLimiter, "auth"))
	mux.Handle("/auth/callback", s.rateLimitByIP(http.HandlerFunc(s.handleOAuthCallback), s.authLimiter, "auth"))
	mux.Handle("/forbidden", s.withPageAuth(http.HandlerFunc(s.handleAccessDeniedPage)))
	mux.HandleFunc("/auth/logout", s.handleLogout)
	mux.HandleFunc("/og-image.svg", s.handleOGImage)
	mux.HandleFunc("/manifest.webmanifest", s.handleWebManifest)
	mux.HandleFunc("/service-worker.js", s.handleServiceWorker)
	mux.Handle("/pwa-icons/", noDirectoryListing(http.StripPrefix("/pwa-icons/", http.FileServer(http.Dir(filepath.Join("static", "favicon"))))))
	mux.Handle("/share-target", s.withPageAuth(http.HandlerFunc(s.handleShareTarget)))
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)
	mux.Handle("/api/dashboard", s.withAPIAuth(http.HandlerFunc(s.handleDashboard), permissionView))
	mux.Handle("/api/users", s.withAPIAuth(http.HandlerFunc(s.handleUsers), permissionManageUsers))
	mux.Handle("/api/users/", s.withAPIAuth(http.HandlerFunc(s.handleUserByID), permissionManageUsers))
	mux.Handle("/api/admin/dashboard", s.withAPIAuth(http.HandlerFunc(s.handleAdminDashboard), permissionManageUsers))
	mux.Handle("/api/admin/backup/status", s.withAPIAuth(http.HandlerFunc(s.handleBackupStatus), permissionManageUsers))
	mux.Handle("/api/admin/backup/export", s.withAPIAuth(http.HandlerFunc(s.handleBackupExport), permissionManageUsers))
	mux.Handle("/api/admin/backup/download", s.withAPIAuth(http.HandlerFunc(s.handleBackupDownload), permissionManageUsers))
	mux.Handle("/api/admin/backup/import", s.withAPIAuth(http.HandlerFunc(s.handleBackupImport), permissionManageUsers))
	mux.Handle("/api/admin/backup/import/parts", s.withAPIAuth(http.HandlerFunc(s.handleBackupImportParts), permissionManageUsers))
	mux.Handle("/api/admin/backup/import/parts/manifest", s.withAPIAuth(http.HandlerFunc(s.handleBackupImportManifest), permissionManageUsers))
	mux.Handle("/api/admin/backup/import/parts/restore", s.withAPIAuth(http.HandlerFunc(s.handleBackupImportPartsRestore), permissionManageUsers))
	mux.Handle("/api/admin/migration", s.withAPIAuth(http.HandlerFunc(s.handleAdminMigration), permissionManageUsers))
	mux.Handle("/api/admin/migration/token", s.withAPIAuth(http.HandlerFunc(s.handleAdminMigrationToken), permissionManageUsers))
	mux.Handle("/api/admin/migration/probe", s.withAPIAuth(http.HandlerFunc(s.handleAdminMigrationProbe), permissionManageUsers))
	mux.Handle("/api/admin/migration/import", s.withAPIAuth(http.HandlerFunc(s.handleAdminMigrationImport), permissionManageUsers))
	mux.Handle("/api/migration/info", s.withMigrationToken(s.handleMigrationInfo))
	mux.Handle("/api/migration/export", s.withMigrationToken(s.handleMigrationExport))
	mux.Handle("/api/admin/audiobook-series/", s.withAPIAuth(http.HandlerFunc(s.handleAdminAudiobookSeries), permissionManageUsers))
	mux.Handle("/api/admin/audiobook-tags", s.withAPIAuth(http.HandlerFunc(s.handleAdminAudiobookTags), permissionManageUsers))
	mux.Handle("/api/admin/audit-logs", s.withAPIAuth(http.HandlerFunc(s.handleAuditLogs), permissionManageUsers))
	mux.Handle("/api/admin/tag-hygiene", s.withAPIAuth(http.HandlerFunc(s.handleTagHygiene), permissionManageUsers))
	mux.Handle("/api/admin/tag-suggestions/status", s.withAPIAuth(http.HandlerFunc(s.handleTagSuggestionStatus), permissionManageUsers))
	mux.Handle("/api/admin/tag-suggestions/reset", s.withAPIAuth(http.HandlerFunc(s.handleResetTagSuggestions), permissionManageUsers))
	mux.Handle("/api/admin/tag-suggestions/retry-failed", s.withAPIAuth(http.HandlerFunc(s.handleRetryFailedTagSuggestions), permissionManageUsers))
	mux.Handle("/api/admin/link-downloads/status", s.withAPIAuth(http.HandlerFunc(s.handleLinkDownloadStatus), permissionManageUsers))
	mux.Handle("/api/admin/link-downloads/", s.withAPIAuth(http.HandlerFunc(s.handleLinkDownloadActions), permissionManageUsers))
	mux.Handle("/api/admin/memes/pending-delete", s.withAPIAuth(http.HandlerFunc(s.handlePendingDeleteQueue), permissionManageUsers))
	mux.Handle("/api/admin/memes/", s.withAPIAuth(http.HandlerFunc(s.handleAdminMemeActions), permissionManageUsers))
	mux.Handle("/api/admin/shares", s.withAPIAuth(http.HandlerFunc(s.handleAdminShares), permissionManageUsers))
	mux.Handle("/api/admin/shares/", s.withAPIAuth(http.HandlerFunc(s.handleAdminShares), permissionManageUsers))
	mux.Handle("/api/admin/audiobook-remux/status", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookRemuxStatus), permissionManageUsers))
	mux.Handle("/api/admin/storage", s.withAPIAuth(http.HandlerFunc(s.handleAdminStorage), permissionManageUsers))
	mux.Handle("/api/admin/storage/orphans/scan", s.withAPIAuth(http.HandlerFunc(s.handleStorageOrphanScan), permissionManageUsers))
	mux.Handle("/api/admin/storage/orphans/delete", s.withAPIAuth(http.HandlerFunc(s.handleStorageOrphanDelete), permissionManageUsers))
	mux.Handle("/api/admin/audiobooks", s.withAPIAuth(http.HandlerFunc(s.handleAdminAudiobooks), permissionManageUsers))
	mux.Handle("/api/admin/user-activity", s.withAPIAuth(http.HandlerFunc(s.handleAdminUserActivity), permissionManageUsers))
	mux.Handle("/api/admin/audiobooks/uploads/", s.withAPIAuth(http.HandlerFunc(s.handleAdminAudiobookUpload), permissionManageUsers))
	mux.Handle("/uploads/", s.withProtectedAssetAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.managers.UploadDir())))))
	if thumbnailDir := s.managers.ThumbnailDir(); strings.TrimSpace(thumbnailDir) != "" {
		mux.Handle("/thumbnails/", s.withProtectedAssetAuth(http.StripPrefix("/thumbnails/", http.FileServer(http.Dir(thumbnailDir)))))
	}
	mux.Handle("/static/", s.withPageAuth(staticAssetHandler("static")))
	mux.HandleFunc("/m/", s.handleMemeLink)
	mux.HandleFunc("/s/", s.handleShareLink)
	mux.Handle("/", s.withPageAuth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("/api/memes", s.withAPIAuth(http.HandlerFunc(s.handleMemes), permissionView))
	mux.Handle("/api/memes/save-link", s.withAPIAuth(http.HandlerFunc(s.handleSaveLink), permissionUpload))
	mux.Handle("/api/memes/", s.withAPIAuth(http.HandlerFunc(s.handleMemeByID), permissionView))
	mux.Handle("/api/memes/random", s.withAPIAuth(http.HandlerFunc(s.handleRandomMeme), permissionView))
	mux.Handle("/api/reel-session", s.withAPIAuth(http.HandlerFunc(s.handleReelSession), permissionView))
	mux.Handle("/api/tags/popular", s.withAPIAuth(http.HandlerFunc(s.handlePopularTags), permissionView))
	mux.Handle("/api/tags", s.withAPIAuth(http.HandlerFunc(s.handleTags), permissionView))
	mux.Handle("/api/audiobooks", s.withAPIAuth(http.HandlerFunc(s.handleAudiobooks), permissionViewAudiobooks))
	mux.Handle("/api/audiobooks/uploads", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookUploads), permissionUploadAudiobooks))
	mux.Handle("/api/audiobooks/uploads/", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookUploadByID), permissionUploadAudiobooks))
	mux.Handle("/api/audiobooks/stats/overview", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookStatsOverview), permissionViewAudiobooks))
	mux.Handle("/api/audiobooks/", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookByID), permissionViewAudiobooks))
	mux.Handle("/api/audiobook-series", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookSeries), permissionViewAudiobooks))
	mux.Handle("/api/audiobook-series/suggestions", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookSeriesSuggestions), permissionViewAudiobooks))
	mux.Handle("/api/audiobook-series/", s.withAPIAuth(http.HandlerFunc(s.handleAudiobookSeriesByID), permissionViewAudiobooks))
	mux.Handle("/audiobook-media/", s.withAudiobookMediaAuth(http.HandlerFunc(s.handleAudiobookMedia)))
	mux.Handle("/audiobook-cover/", s.withAudiobookMediaAuth(http.HandlerFunc(s.handleAudiobookCover)))
	return securityHeaders(http.NewCrossOriginProtection().Handler(s.maintenanceGuard(mux)))
}

// maintenanceGuard rejects state-changing requests with 503 while a portable
// backup restore is swapping the database contents and the uploads/thumbnails
// directories underneath the running server. Reads stay available. The restore
// endpoint itself is exempt so the in-progress import can complete and so a
// second import attempt still receives a precise 409 from handleBackupImport.
func (s *Server) maintenanceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.restoring.Load() && isMutatingMethod(r.Method) && r.URL.Path != "/api/admin/backup/import" && r.URL.Path != "/api/admin/backup/import/parts/restore" && r.URL.Path != "/api/admin/migration/import" {
			w.Header().Set("Retry-After", "30")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "MemeIndex is temporarily read-only while a backup restore finishes; retry shortly",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isMutatingMethod reports whether an HTTP method can change server state.
func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func currentAuditActor(r *http.Request) accessor.AuditActor {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return accessor.AuditActor{}
	}

	return accessor.AuditActor{
		UserID:       session.UserID,
		Username:     session.Username,
		DisplayName:  session.DisplayName,
		AvatarURL:    session.AvatarURL,
		IsSuperAdmin: session.Permissions.CanManageUsers,
	}
}

func diffTags(current []string, next []string) ([]string, []string) {
	currentSet := map[string]struct{}{}
	nextSet := map[string]struct{}{}
	for _, tag := range current {
		currentSet[tag] = struct{}{}
	}
	for _, tag := range next {
		nextSet[tag] = struct{}{}
	}

	added := []string{}
	removed := []string{}
	for tag := range nextSet {
		if _, exists := currentSet[tag]; !exists {
			added = append(added, tag)
		}
	}
	for tag := range currentSet {
		if _, exists := nextSet[tag]; !exists {
			removed = append(removed, tag)
		}
	}
	return added, removed
}

func contextWithSession(ctx context.Context, session authSession) context.Context {
	return context.WithValue(ctx, authSessionContextKey, session)
}

func currentUserID(r *http.Request) string {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return ""
	}
	return session.UserID
}

// jsonResponseBufferPool recycles the scratch buffers writeJSON marshals into.
// Serializing into a buffer first (instead of streaming json.Encoder straight
// down the socket) lets every JSON response carry a Content-Length and go out in
// a single Write rather than a chunked dribble of ~4 KB flushes.
var jsonResponseBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// maxPooledJSONBuffer bounds the capacity of a buffer kept for reuse so a single
// large response (a long audit feed, a backup manifest) cannot pin megabytes in
// the pool for the life of the process.
const maxPooledJSONBuffer = 1 << 20

func writeJSON(w http.ResponseWriter, status int, payload any) {
	buf := jsonResponseBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= maxPooledJSONBuffer {
			jsonResponseBufferPool.Put(buf)
		}
	}()

	// json.Encoder appends a trailing newline; harmless and matches the previous
	// behaviour. Nothing has been written to w yet, so an encode failure can still
	// surface as a clean error status.
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		log.Printf("write JSON failed: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write JSON failed: %v", err)
	}
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
