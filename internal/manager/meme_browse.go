package manager

import (
	"context"
	"fmt"
	"math/rand/v2"
	"memeindex/internal/accessor"
	"slices"
	"strings"
)

type MemeCounts struct {
	Total     int `json:"total"`
	Favorites int `json:"favorites"`
	Videos    int `json:"videos"`
	Images    int `json:"images"`
	MP3s      int `json:"mp3s"`
	Untagged  int `json:"untagged"`
	Files     int `json:"files"`
}

type MemeListResult struct {
	Memes      []accessor.Meme `json:"memes"`
	Counts     MemeCounts      `json:"counts"`
	HasMore    bool            `json:"has_more"`
	NextOffset int             `json:"next_offset"`
}

// VaultDashboard is intentionally derived from the current store on every request.
// It keeps the user home screen honest without introducing a second statistics
// cache that could drift from the archive.
type VaultDashboard struct {
	TotalItems    int             `json:"total_items"`
	Favorites     int             `json:"favorites"`
	StorageBytes  int64           `json:"storage_bytes"`
	TagCount      int             `json:"tag_count"`
	Counts        MemeCounts      `json:"counts"`
	RecentItems   []accessor.Meme `json:"recent_items"`
	FavoriteItems []accessor.Meme `json:"favorite_items"`
	RandomItems   []accessor.Meme `json:"random_items"`
	TopTags       []VaultTagStat  `json:"top_tags"`
}

type VaultTagStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// maxMemePageLimit caps a single browse/search page so a caller-supplied limit
// cannot ask the database (or the in-process test store) for an unbounded slice.
const maxMemePageLimit = 500

func (m *MemeManager) ListMemes(ctx context.Context, userID, query string, favoritesOnly bool, tag string, view string, offset int, limit int) (MemeListResult, error) {
	return m.ListMemesSorted(ctx, userID, query, favoritesOnly, tag, view, "newest", offset, limit)
}

// ListMemesSorted resolves one page of a browse or search. A store that can
// query (Postgres) answers it in SQL, and its errors are returned rather than
// retried as a full in-memory scan: that scan would load the whole archive
// while the database is already struggling, and it does not understand the
// search operators, so results would silently change. Only a store that
// cannot query at all (the in-process test store) is scanned in memory.
func (m *MemeManager) ListMemesSorted(ctx context.Context, userID, query string, favoritesOnly bool, tag string, view string, sortBy string, offset int, limit int) (MemeListResult, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 72
	}
	if limit > maxMemePageLimit {
		limit = maxMemePageLimit
	}

	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		page, err := qs.QueryMemes(ctx, accessor.MemeQuery{
			UserID:        strings.TrimSpace(userID),
			Search:        strings.TrimSpace(query),
			Tag:           strings.TrimSpace(tag),
			View:          strings.TrimSpace(view),
			FavoritesOnly: favoritesOnly,
			Sort:          sortBy,
			Offset:        offset,
			Limit:         limit,
		})
		if err != nil {
			return MemeListResult{}, fmt.Errorf("query memes: %w", err)
		}
		memes := page.Memes
		if memes == nil {
			memes = []accessor.Meme{}
		}
		return MemeListResult{
			Memes:      memes,
			Counts:     memeCountsFromCategory(page.Counts),
			HasMore:    page.HasMore,
			NextOffset: page.NextOffset,
		}, nil
	}

	source := m.store.List(ctx, strings.TrimSpace(userID), strings.TrimSpace(query), false, strings.TrimSpace(tag))
	counts := buildMemeCounts(source)
	visible := filterMemesByView(source, strings.TrimSpace(view))
	if favoritesOnly {
		visible = filterMemesByView(visible, "favorites")
	}
	sortMemes(visible, sortBy)

	start := min(offset, len(visible))
	end := min(offset+limit, len(visible))
	page := visible[start:end]

	return MemeListResult{
		Memes:      append([]accessor.Meme(nil), page...),
		Counts:     counts,
		HasMore:    end < len(visible),
		NextOffset: end,
	}, nil
}

func memeCountsFromCategory(c accessor.MemeCategoryCounts) MemeCounts {
	return MemeCounts{
		Total:     c.Total,
		Favorites: c.Favorites,
		Videos:    c.Videos,
		Images:    c.Images,
		MP3s:      c.MP3s,
		Untagged:  c.Untagged,
		Files:     c.Files,
	}
}

// Dashboard builds the home screen summary. As with ListMemesSorted, a
// querying store's error is returned instead of falling back to a full scan.
func (m *MemeManager) Dashboard(ctx context.Context, userID string) (VaultDashboard, error) {
	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		data, err := qs.MemeDashboard(ctx, strings.TrimSpace(userID))
		if err != nil {
			return VaultDashboard{}, fmt.Errorf("query dashboard: %w", err)
		}
		return vaultDashboardFromData(data), nil
	}

	items := m.store.List(ctx, strings.TrimSpace(userID), "", false, "")
	dashboard := VaultDashboard{
		Counts:        buildMemeCounts(items),
		RecentItems:   []accessor.Meme{},
		FavoriteItems: []accessor.Meme{},
		RandomItems:   []accessor.Meme{},
		TopTags:       []VaultTagStat{},
	}
	tagCounts := map[string]int{}
	for _, item := range items {
		dashboard.TotalItems++
		dashboard.StorageBytes += item.SizeBytes
		if item.Favorite {
			dashboard.Favorites++
			if len(dashboard.FavoriteItems) < accessor.DashboardFavoriteLimit {
				dashboard.FavoriteItems = append(dashboard.FavoriteItems, item)
			}
		}
		for _, tag := range item.Tags {
			normalized := strings.TrimSpace(tag)
			if normalized != "" {
				tagCounts[normalized]++
			}
		}
	}
	limit := min(6, len(items))
	dashboard.RecentItems = append(dashboard.RecentItems, items[:min(accessor.DashboardRecentLimit, len(items))]...)
	for _, index := range rand.Perm(len(items))[:limit] {
		dashboard.RandomItems = append(dashboard.RandomItems, items[index])
	}
	dashboard.TagCount = len(tagCounts)
	dashboard.TopTags = topTagStats(tagCounts, accessor.DashboardTopTagLimit)
	return dashboard, nil
}

// NewestMemeID returns the id of the most recently added meme the user can
// see, or "" for an empty vault. It is the cheap check behind the home
// screen's "anything new?" poll.
func (m *MemeManager) NewestMemeID(ctx context.Context, userID string) (string, error) {
	page, err := m.ListMemesSorted(ctx, userID, "", false, "", "", "newest", 0, 1)
	if err != nil {
		return "", err
	}
	if len(page.Memes) == 0 {
		return "", nil
	}
	return page.Memes[0].ID, nil
}

func vaultDashboardFromData(data accessor.MemeDashboardData) VaultDashboard {
	dashboard := VaultDashboard{
		TotalItems:    data.TotalItems,
		Favorites:     data.Favorites,
		StorageBytes:  data.StorageBytes,
		TagCount:      data.TagCount,
		Counts:        memeCountsFromCategory(data.Counts),
		RecentItems:   nonNilMemes(data.RecentItems),
		FavoriteItems: nonNilMemes(data.FavoriteItems),
		RandomItems:   nonNilMemes(data.RandomItems),
		TopTags:       vaultTagStats(data.TopTags),
	}
	return dashboard
}

func nonNilMemes(memes []accessor.Meme) []accessor.Meme {
	if memes == nil {
		return []accessor.Meme{}
	}
	return memes
}

func vaultTagStats(tags []accessor.TagCount) []VaultTagStat {
	stats := make([]VaultTagStat, 0, len(tags))
	for _, tag := range tags {
		stats = append(stats, VaultTagStat{Name: tag.Name, Count: tag.Count})
	}
	return stats
}

// PopularTags returns the most used tags. A querying store's error is returned
// instead of falling back to a full scan.
func (m *MemeManager) PopularTags(ctx context.Context, userID string, limit int) ([]VaultTagStat, error) {
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)

	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		tags, err := qs.TagCounts(ctx, limit)
		if err != nil {
			return nil, fmt.Errorf("query tag counts: %w", err)
		}
		return vaultTagStats(tags), nil
	}

	tagCounts := map[string]int{}
	for _, item := range m.store.List(ctx, strings.TrimSpace(userID), "", false, "") {
		for _, tag := range item.Tags {
			normalized := strings.TrimSpace(tag)
			if normalized != "" {
				tagCounts[normalized]++
			}
		}
	}
	return topTagStats(tagCounts, limit), nil
}

func topTagStats(tagCounts map[string]int, limit int) []VaultTagStat {
	stats := make([]VaultTagStat, 0, len(tagCounts))
	for name, count := range tagCounts {
		stats = append(stats, VaultTagStat{Name: name, Count: count})
	}
	slices.SortFunc(stats, func(a, b VaultTagStat) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Name, b.Name)
	})
	if limit >= 0 && len(stats) > limit {
		stats = stats[:limit]
	}
	return stats
}

func sortMemes(memes []accessor.Meme, sortBy string) {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "oldest":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return a.CreatedAt.Compare(b.CreatedAt) })
	case "name":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int {
			return strings.Compare(strings.ToLower(a.OriginalName), strings.ToLower(b.OriginalName))
		})
	case "size":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int {
			if a.SizeBytes == b.SizeBytes {
				return 0
			}
			if a.SizeBytes > b.SizeBytes {
				return -1
			}
			return 1
		})
	case "updated":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	default:
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return b.CreatedAt.Compare(a.CreatedAt) })
	}
}

func buildMemeCounts(memes []accessor.Meme) MemeCounts {
	counts := MemeCounts{}
	for _, meme := range memes {
		counts.Total += 1
		if meme.Favorite {
			counts.Favorites += 1
		}
		switch {
		case strings.HasPrefix(meme.ContentType, "video/"):
			counts.Videos += 1
		case strings.HasPrefix(meme.ContentType, "image/"):
			counts.Images += 1
		case meme.ContentType == "audio/mpeg" || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3"):
			counts.MP3s += 1
		default:
			counts.Files += 1
		}
		if len(meme.Tags) == 0 {
			counts.Untagged += 1
		}
	}
	return counts
}

func filterMemesByView(memes []accessor.Meme, view string) []accessor.Meme {
	switch view {
	case "favorites":
		return filterMemes(memes, func(meme accessor.Meme) bool { return meme.Favorite })
	case "videos":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "video/") })
	case "images":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "image/") })
	case "mp3s":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return strings.HasPrefix(meme.ContentType, "audio/") || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	case "untagged":
		return filterMemes(memes, func(meme accessor.Meme) bool { return len(meme.Tags) == 0 })
	case "files":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return !strings.HasPrefix(meme.ContentType, "image/") &&
				!strings.HasPrefix(meme.ContentType, "video/") &&
				!strings.HasPrefix(meme.ContentType, "audio/") &&
				!strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	default:
		return memes
	}
}

func filterMemes(memes []accessor.Meme, keep func(accessor.Meme) bool) []accessor.Meme {
	filtered := make([]accessor.Meme, 0, len(memes))
	for _, meme := range memes {
		if keep(meme) {
			filtered = append(filtered, meme)
		}
	}
	return filtered
}
