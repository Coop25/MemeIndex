package manager

import (
	"os"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

type dashboardMetricStore struct {
	memes              []accessor.Meme
	favoriteTotal      int
	favoriteActivities []accessor.AdminFavoriteActivity
}

func (s *dashboardMetricStore) List(string, string, bool, string) []accessor.Meme {
	return append([]accessor.Meme(nil), s.memes...)
}
func (s *dashboardMetricStore) SuggestTags(string, int) []string { return nil }
func (s *dashboardMetricStore) GetByID(string, string) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *dashboardMetricStore) Random([]string) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *dashboardMetricStore) Create(accessor.CreateInput) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *dashboardMetricStore) Update(string, string, accessor.MemeUpdate) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *dashboardMetricStore) SetFavorite(string, string, bool) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *dashboardMetricStore) Delete(accessor.DeleteInput) (accessor.DeleteResult, error) {
	return accessor.DeleteResult{}, os.ErrNotExist
}
func (s *dashboardMetricStore) UploadDir() string { return os.TempDir() }
func (s *dashboardMetricStore) TotalFavoriteAssignments() (int, error) {
	return s.favoriteTotal, nil
}
func (s *dashboardMetricStore) FavoriteActivitySince(time.Time) ([]accessor.AdminFavoriteActivity, error) {
	return append([]accessor.AdminFavoriteActivity(nil), s.favoriteActivities...), nil
}

func TestAdminDashboardMetricSeriesEndsAtCurrentTotals(t *testing.T) {
	now := time.Now().UTC()
	store := &dashboardMetricStore{
		memes: []accessor.Meme{
			{ID: "new", SizeBytes: 200, Tags: []string{"new-tag", "shared"}, CreatedAt: now},
			{ID: "older", SizeBytes: 100, Tags: []string{"shared"}, CreatedAt: now.AddDate(0, 0, -10)},
		},
		favoriteTotal: 3,
		favoriteActivities: []accessor.AdminFavoriteActivity{
			{Date: now, Added: 1},
		},
	}

	dashboard := NewMemeManager(store).AdminDashboard()
	if len(dashboard.MetricSeries) != 30 {
		t.Fatalf("metric series length = %d, want 30", len(dashboard.MetricSeries))
	}
	last := dashboard.MetricSeries[len(dashboard.MetricSeries)-1]
	if last.Memes != 2 {
		t.Errorf("last meme count = %d, want 2", last.Memes)
	}
	if last.Tags != 2 {
		t.Errorf("last tag count = %d, want 2", last.Tags)
	}
	if last.StorageBytes != 300 {
		t.Errorf("last storage = %d, want 300", last.StorageBytes)
	}
	if last.Favorites != 3 {
		t.Errorf("last favorites = %d, want 3", last.Favorites)
	}
}
