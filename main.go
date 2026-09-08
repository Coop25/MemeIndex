package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/client"
	"memeindex/internal/manager"
	"memeindex/internal/tagsuggest"
)

func main() {
	config, err := client.LoadConfig()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	log.Printf("MemeIndex version: %s", client.BuildVersion())

	var (
		store accessor.Store
	)

	if config.DatabaseURL != "" {
		store, err = accessor.NewPostgresStore(context.Background(), config.DatabaseURL, config.DataDir)
		if err != nil {
			log.Fatalf("postgres store init failed: %v", err)
		}
		log.Printf("MemeIndex storage: postgres")
	} else {
		store, err = accessor.NewMemeStore(config.DataDir)
		if err != nil {
			log.Fatalf("store init failed: %v", err)
		}
		log.Printf("MemeIndex storage: local files")
	}

	tagSuggester := tagsuggest.New(tagsuggest.Config{
		OllamaURL:    config.TagSuggestions.OllamaURL,
		Model:        config.TagSuggestions.Model,
		Timeout:      config.TagSuggestions.Timeout,
		MaxTags:      config.TagSuggestions.MaxTags,
		GenerateOnly: config.TagSuggestions.GenerateOnly,
	})
	tagTranscriber := tagsuggest.NewTranscriber(tagsuggest.TranscriberConfig{
		Binary:  config.TagSuggestions.TranscribeBinary,
		Args:    config.TagSuggestions.TranscribeArgs,
		Timeout: config.TagSuggestions.TranscribeTimeout,
	})
	memeManager := manager.NewMemeManagerWithTagSuggester(
		store,
		tagSuggester,
		tagTranscriber,
		manager.TagSuggestionRuntimeConfig{
			VideoFrameCount:   config.TagSuggestions.VideoFrameCount,
			VideoFrameWidth:   config.TagSuggestions.VideoFrameWidth,
			DisableTranscript: config.TagSuggestions.DisableTranscript,
		},
		config.TagSuggestions.KnownTagBudget,
	)
	go runPreviewAssetBackfill(memeManager)
	memeManager.StartTagHygieneWorker()
	memeManager.StartTagSuggestionWorker()
	if queued := memeManager.SeedTagSuggestionQueue(); queued > 0 {
		log.Printf("tag suggestion worker: queued %d existing untagged meme(s) with no pending suggestions", queued)
	}
	go runNightlyReelSessionCleanup(memeManager)
	server := client.NewServer(config, memeManager)

	httpServer := &http.Server{
		Addr:              config.Addr,
		Handler:           client.LoggingMiddleware(client.MetricsMiddleware(client.AssetCompression(server.Routes()))),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	debugServer := newDebugServer(config.DebugAddr)
	if debugServer != nil {
		go func() {
			log.Printf("MemeIndex debug listener on http://%s (/metrics, /debug/pprof/) - keep this bound to a private interface", config.DebugAddr)
			if err := debugServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("debug listener stopped: %v", err)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("MemeIndex listening on http://localhost%s", config.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		log.Fatalf("http server failed: %v", err)
	case <-ctx.Done():
		// Restore default signal handling so a second Ctrl+C/SIGTERM during a
		// slow drain force-quits instead of hanging.
		stop()
		log.Printf("shutdown signal received; draining in-flight requests")
	}

	server.BeginDraining()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown timed out, forcing close: %v", err)
		_ = httpServer.Close()
	}
	if debugServer != nil {
		_ = debugServer.Shutdown(shutdownCtx)
	}

	if closer, ok := store.(interface{ Close() }); ok {
		closer.Close()
		log.Printf("storage connections closed")
	}

	log.Printf("shutdown complete")
}

// newDebugServer builds the private diagnostics listener (Prometheus /metrics
// and net/http/pprof). It returns nil when MEMEINDEX_DEBUG_ADDR is unset, which
// is the default: nothing extra is exposed unless an operator opts in. Bind it
// to a loopback or private address only.
func newDebugServer(addr string) *http.Server {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", client.MetricsHandler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func runNightlyReelSessionCleanup(memeManager *manager.MemeManager) {
	for {
		now := time.Now().UTC()
		nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		time.Sleep(time.Until(nextRun))

		if err := memeManager.CleanupStaleReelSessions(); err != nil {
			log.Printf("nightly reel session cleanup failed: %v", err)
			continue
		}

		log.Printf("nightly reel session cleanup completed")
	}
}

func runPreviewAssetBackfill(memeManager *manager.MemeManager) {
	if err := memeManager.EnsurePreviewAssets(); err != nil {
		log.Printf("preview asset backfill failed: %v", err)
		return
	}
	log.Printf("preview asset backfill completed")
}
