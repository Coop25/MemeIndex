package manager

import (
	"fmt"
	"testing"

	"memeindex/internal/accessor"
)

// The home screen fills a whole row with favorites and draws a tag cloud, so
// the in-memory dashboard must hand over up to the shared limits rather than
// the handful the old fixed cards needed.
func TestDashboardFavoriteAndTagLimits(t *testing.T) {
	const total = accessor.DashboardFavoriteLimit + 6
	memes := make([]accessor.Meme, 0, total)
	for i := 0; i < total; i++ {
		memes = append(memes, accessor.Meme{
			ID:          fmt.Sprintf("m%d", i),
			ContentType: "image/png",
			Favorite:    true,
			Tags:        []string{fmt.Sprintf("tag%d", i)},
		})
	}
	m := NewMemeManager(&adminReadCacheStore{memes: memes})

	got := m.Dashboard("u")

	if got.Favorites != total {
		t.Fatalf("Favorites = %d, want %d", got.Favorites, total)
	}
	if len(got.FavoriteItems) != accessor.DashboardFavoriteLimit {
		t.Fatalf("FavoriteItems = %d, want %d", len(got.FavoriteItems), accessor.DashboardFavoriteLimit)
	}
	if got.TagCount != total {
		t.Fatalf("TagCount = %d, want %d", got.TagCount, total)
	}
	if len(got.TopTags) != accessor.DashboardTopTagLimit {
		t.Fatalf("TopTags = %d, want %d", len(got.TopTags), accessor.DashboardTopTagLimit)
	}
}
