package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/client"
	"memeindex/internal/manager"
)

func main() {
	config, err := client.LoadConfig()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

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

	memeManager := manager.NewMemeManager(store)
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
