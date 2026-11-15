package client

import (
	"context"
	"sync"
	"time"

	"memeindex/internal/audiobooks"
	"memeindex/internal/manager"
)

// audiobookAdminStatsCache memoizes AdminStats for manager.AdminReadCacheTTL.
type audiobookAdminStatsCache struct {
	mu       sync.Mutex
	stats    audiobooks.AdminStatsReport
	cachedAt time.Time
	valid    bool
}

// cachedAudiobookAdminStats returns the audiobook admin aggregate, or nil when
// audiobooks are not configured.
func (s *Server) cachedAudiobookAdminStats(ctx context.Context) (*audiobooks.AdminStatsReport, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	c := &s.audiobookAdminStats
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < manager.AdminReadCacheTTL {
		out := c.stats
		out.Series = append([]audiobooks.AdminDayStat(nil), c.stats.Series...)
		return &out, nil
	}
	stats, err := s.audiobooks.AdminStats(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	c.stats, c.cachedAt, c.valid = stats, time.Now(), true
	out := stats
	out.Series = append([]audiobooks.AdminDayStat(nil), stats.Series...)
	return &out, nil
}
