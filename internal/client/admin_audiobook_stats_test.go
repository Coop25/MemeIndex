package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func insertAdminStatsBook(t *testing.T, store *postgresAudiobookStore, size int64, duration float64, createdAt time.Time, coverPath string) {
	t.Helper()
	id := uuid.Must(uuid.NewV6()).String()
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, cover_path, uploaded_by_user_id, created_at)
		VALUES ($1, 'Book', 'book.mp3', $2, $3, '/tmp/book.mp3', $4, 'u-admin', $5)
	`, id, size, duration, coverPath, createdAt)
	if err != nil {
		t.Fatalf("insert audiobook: %v", err)
	}
}

func TestAudiobookAdminStats(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	coverPath := filepath.Join(store.coversDir, "cover.jpg")
	if err := os.WriteFile(coverPath, make([]byte, 10), 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	day := 24 * time.Hour
	insertAdminStatsBook(t, store, 100, 3600, now.Add(-2*day), coverPath)
	insertAdminStatsBook(t, store, 200, 3600, now.Add(-40*day), filepath.Join(store.coversDir, "missing.jpg"))
	insertAdminStatsBook(t, store, 300, 3600, now.Add(-70*day), "")
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO audiobook_uploads (id, user_id, title, filename, mime_type, total_bytes, received_bytes, temp_path)
		VALUES ($1, 'u-admin', 'T', 'f.mp3', 'audio/mpeg', 500, 50, '/tmp/x')
	`, uuid.Must(uuid.NewV6()).String()); err != nil {
		t.Fatalf("insert upload: %v", err)
	}

	got, err := store.AdminStats(ctx, now)
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}
	checks := []struct {
		name      string
		got, want int64
	}{
		{"Count", int64(got.Count), 3},
		{"AddedLast30Days", int64(got.AddedLast30Days), 1},
		{"AddedPrev30Days", int64(got.AddedPrevious30Days), 1},
		{"DurationSeconds", int64(got.DurationSeconds), 10800},
		{"MediaBytes", got.MediaBytes, 600},
		{"CoverBytes", got.CoverBytes, 10},
		{"UploadTempBytes", got.UploadTempBytes, 50},
		{"TotalBytes", got.TotalBytes, 660},
		{"BytesLast30Days", got.BytesLast30Days, 100},
		{"BytesPrev30Days", got.BytesPrevious30Days, 200},
		{"len(Series)", int64(len(got.Series)), 30},
		{"Series[29].Count", int64(got.Series[29].Count), 3},
		{"Series[0].Count", int64(got.Series[0].Count), 2},
		{"Series[29].Bytes", got.Series[29].Bytes, 600},
		{"Series[0].Bytes", got.Series[0].Bytes, 500},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if want := now.UTC().Format("2006-01-02"); got.Series[29].Date != want {
		t.Errorf("Series[29].Date = %s, want %s", got.Series[29].Date, want)
	}
	if want := fmt.Sprint(now.UTC().AddDate(0, 0, -29).Format("2006-01-02")); got.Series[0].Date != want {
		t.Errorf("Series[0].Date = %s, want %s", got.Series[0].Date, want)
	}
}

func TestAudiobookAdminStatsEmpty(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	got, err := store.AdminStats(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("AdminStats: %v", err)
	}
	if got.Count != 0 || got.TotalBytes != 0 || got.MediaBytes != 0 || got.DurationSeconds != 0 || got.AddedLast30Days != 0 {
		t.Errorf("expected zeros, got %+v", got)
	}
	if len(got.Series) != 30 {
		t.Errorf("len(Series) = %d, want 30", len(got.Series))
	}
}
