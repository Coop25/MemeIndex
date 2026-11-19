package client

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"memeindex/internal/audiobooks"
	"memeindex/internal/manager"
)

type adminAudiobookActivity struct {
	DailySeconds []audiobooks.AdminDaySeconds  `json:"daily_seconds"`
	TopBooks     []audiobooks.AdminTopBook     `json:"top_books"`
	TopListeners []audiobooks.AdminTopListener `json:"top_listeners"`
	Health       audiobooks.AdminHealth        `json:"health"`
	// Partial is set when some aggregate queries failed; it is cached with
	// the result so a degraded answer is never served as fully healthy.
	Partial bool `json:"-"`
}

// adminWorkQueues are the "waiting on an admin or a worker" counts.
type adminWorkQueues struct {
	DeleteQueue     int `json:"delete_queue"`
	SuggestedTags   int `json:"suggested_tags"`
	LinkRetries     int `json:"link_retries"`
	RejectedLinks   int `json:"rejected_links"`
	ConversionQueue int `json:"conversion_queue"`
}

// failedRemuxAudiobookIDs returns the IDs of stored MP3 audiobooks whose
// conversion left a failure marker next to the file.
func (s *Server) failedRemuxAudiobookIDs(ctx context.Context) ([]string, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	books, err := s.audiobooks.ListStoredMP3s(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, book := range books {
		if _, statErr := os.Stat(audiobookRemuxFailurePath(book.StoragePath)); statErr == nil {
			ids = append(ids, book.ID)
		}
	}
	return ids, nil
}

// audiobookActivityCache memoizes the audiobook activity aggregate for
// manager.AdminReadCacheTTL.
type audiobookActivityCache struct {
	mu       sync.Mutex
	activity adminAudiobookActivity
	cachedAt time.Time
	valid    bool
}

// invalidate drops the memoized aggregate, so the next read recomputes it.
func (c *audiobookActivityCache) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

func cloneAdminAudiobookActivity(a adminAudiobookActivity) adminAudiobookActivity {
	a.DailySeconds = append([]audiobooks.AdminDaySeconds(nil), a.DailySeconds...)
	a.TopBooks = append([]audiobooks.AdminTopBook(nil), a.TopBooks...)
	a.TopListeners = append([]audiobooks.AdminTopListener(nil), a.TopListeners...)
	return a
}

// cachedAudiobookActivity returns the listening/health aggregate (listener
// display names empty), or nil when audiobooks are not configured.
func (s *Server) cachedAudiobookActivity(ctx context.Context) (*adminAudiobookActivity, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	c := &s.audiobookActivity
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < manager.AdminReadCacheTTL {
		out := cloneAdminAudiobookActivity(c.activity)
		return &out, nil
	}
	var activity adminAudiobookActivity
	failures := 0
	if daily, books, listeners, err := s.audiobooks.AdminListening(ctx, time.Now()); err != nil {
		failures++
		log.Printf("admin dashboard audiobook listening failed: %v", err)
		activity.DailySeconds, activity.TopBooks, activity.TopListeners = []audiobooks.AdminDaySeconds{}, []audiobooks.AdminTopBook{}, []audiobooks.AdminTopListener{}
	} else {
		activity.DailySeconds, activity.TopBooks, activity.TopListeners = daily, books, listeners
	}
	if health, err := s.audiobooks.AdminHealthCounts(ctx); err != nil {
		failures++
		log.Printf("admin dashboard audiobook health failed: %v", err)
	} else {
		activity.Health = health
	}
	if failed, err := s.failedRemuxAudiobookIDs(ctx); err != nil {
		failures++
		log.Printf("admin dashboard failed conversions failed: %v", err)
	} else {
		activity.Health.FailedConversion = len(failed)
		// Permanently failed books are not "awaiting" conversion.
		activity.Health.AwaitingConversion -= len(failed)
		if activity.Health.AwaitingConversion < 0 {
			activity.Health.AwaitingConversion = 0
		}
	}
	if failures == 3 {
		return nil, fmt.Errorf("all audiobook activity queries failed")
	}
	activity.Partial = failures > 0
	c.activity, c.cachedAt, c.valid = activity, time.Now(), true
	out := cloneAdminAudiobookActivity(activity)
	return &out, nil
}
