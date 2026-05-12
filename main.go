package main

import (
	"context"
	"log"
	"net/http"
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
	memeManager.StartTagSuggestionWorker()
	if queued := memeManager.SeedTagSuggestionQueue(); queued > 0 {
		log.Printf("tag suggestion worker: queued %d existing untagged meme(s) with no pending suggestions", queued)
	}
	go runNightlyReelSessionCleanup(memeManager)
	server := client.NewServer(config, memeManager)

	log.Printf("MemeIndex listening on http://localhost%s", config.Addr)
	if err := http.ListenAndServe(config.Addr, client.LoggingMiddleware(server.Routes())); err != nil {
		log.Fatal(err)
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
