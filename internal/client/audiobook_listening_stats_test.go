package client

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAudiobookListeningStreak covers the pure streak-counting logic, with no
// database involved.
func TestAudiobookListeningStreak(t *testing.T) {
	now := time.Date(2024, 3, 10, 15, 0, 0, 0, time.UTC) // a Sunday

	t.Run("counts back from today when today has an entry", func(t *testing.T) {
		listenedOn := map[string]bool{
			"2024-03-10": true,
			"2024-03-09": true,
			"2024-03-08": true,
			"2024-03-06": true, // gap on the 7th breaks the streak here
		}
		if got := audiobookListeningStreak(listenedOn, now); got != 3 {
			t.Fatalf("streak = %d, want 3", got)
		}
	})

	t.Run("falls back to yesterday when today has no entry yet", func(t *testing.T) {
		listenedOn := map[string]bool{
			"2024-03-09": true,
			"2024-03-08": true,
		}
		if got := audiobookListeningStreak(listenedOn, now); got != 2 {
			t.Fatalf("streak = %d, want 2", got)
		}
	})

	t.Run("zero when neither today nor yesterday has an entry", func(t *testing.T) {
		listenedOn := map[string]bool{
			"2024-03-01": true,
		}
		if got := audiobookListeningStreak(listenedOn, now); got != 0 {
			t.Fatalf("streak = %d, want 0", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		if got := audiobookListeningStreak(map[string]bool{}, now); got != 0 {
			t.Fatalf("streak = %d, want 0", got)
		}
	})
}

// newTestAudiobookStore opens a real Postgres-backed store for integration
// tests, skipping unless MEMEINDEX_TEST_DATABASE_URL is set. See
// internal/accessor/postgres_store_integration_test.go for how to run one
// locally.
func newTestAudiobookStore(t *testing.T) *postgresAudiobookStore {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	st, err := newAudiobookStore(context.Background(), dsn, t.TempDir())
	if err != nil {
		t.Fatalf("newAudiobookStore: %v", err)
	}
	store := st.(*postgresAudiobookStore)
	t.Cleanup(store.pool.Close)
	return store
}

func insertTestAudiobook(t *testing.T, store *postgresAudiobookStore, durationSeconds float64) (audiobookID, userID string) {
	t.Helper()
	ctx := context.Background()
	audiobookID = uuid.Must(uuid.NewV6()).String()
	userID = "u-" + uuid.Must(uuid.NewV6()).String()
	_, err := store.pool.Exec(ctx, `
		INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, uploaded_by_user_id)
		VALUES ($1, 'Test Book', 'test.mp3', 1, $2, '/tmp/test.mp3', $3)
	`, audiobookID, durationSeconds, userID)
	if err != nil {
		t.Fatalf("insert test audiobook: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM audiobooks WHERE id = $1`, audiobookID)
	})
	return audiobookID, userID
}

// TestHeartbeatAccumulatesListeningStats covers that a continuous run of
// "playing" heartbeats accumulates roughly the real elapsed time into today's
// listening-stats bucket, and that a gap longer than
// audiobookListeningStatsMaxGap is clamped instead of fully credited.
func TestHeartbeatAccumulatesListeningStats(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	audiobookID, userID := insertTestAudiobook(t, store, 3600)
	deviceID := "dev-" + uuid.Must(uuid.NewV6()).String()

	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceID, 0, true, true); err != nil {
		t.Fatalf("heartbeat 1: %v", err)
	}

	// Back-date the session's updated_at to simulate ~2s of real playback
	// between heartbeats, without the test sleeping.
	if _, err := store.pool.Exec(ctx, `
		UPDATE audiobook_playback_sessions SET updated_at = NOW() - INTERVAL '2 seconds'
		WHERE audiobook_id = $1 AND user_id = $2 AND device_id = $3
	`, audiobookID, userID, deviceID); err != nil {
		t.Fatalf("back-date heartbeat 1: %v", err)
	}
	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceID, 2, true, false); err != nil {
		t.Fatalf("heartbeat 2: %v", err)
	}

	// Simulate a long gap (suspended tab) before the next heartbeat - this
	// must be clamped to audiobookListeningStatsMaxGap, not credited in full.
	if _, err := store.pool.Exec(ctx, `
		UPDATE audiobook_playback_sessions SET updated_at = NOW() - INTERVAL '10 minutes'
		WHERE audiobook_id = $1 AND user_id = $2 AND device_id = $3
	`, audiobookID, userID, deviceID); err != nil {
		t.Fatalf("back-date heartbeat 2: %v", err)
	}
	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceID, 602, true, false); err != nil {
		t.Fatalf("heartbeat 3: %v", err)
	}

	history, err := store.GetBookListeningHistory(ctx, audiobookID, userID)
	if err != nil {
		t.Fatalf("GetBookListeningHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history has %d days, want 1 (all heartbeats landed today): %+v", len(history), history)
	}
	// ~2s from the first gap, clamped 30s from the second gap: expect
	// something in [30, 34] seconds, well short of the unclamped ~602s.
	got := history[0].SecondsListened
	if got < 30 || got > 34 {
		t.Fatalf("seconds_listened = %v, want roughly 32 (2s + capped 30s)", got)
	}
}

// TestHeartbeatDoesNotAccumulateWhilePaused covers that heartbeats reported
// as not-playing never add listening time, even back to back.
func TestHeartbeatDoesNotAccumulateWhilePaused(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	audiobookID, userID := insertTestAudiobook(t, store, 3600)
	deviceID := "dev-" + uuid.Must(uuid.NewV6()).String()

	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceID, 0, false, false); err != nil {
		t.Fatalf("heartbeat 1: %v", err)
	}
	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceID, 0, false, false); err != nil {
		t.Fatalf("heartbeat 2: %v", err)
	}

	history, err := store.GetBookListeningHistory(ctx, audiobookID, userID)
	if err != nil {
		t.Fatalf("GetBookListeningHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %+v, want none (never reported playing)", history)
	}
}

// TestSaveProgressSetsFinishedAtOnce covers that finished_at is stamped the
// first time saved progress crosses the finished threshold, and is left
// untouched by later progress updates (including relistening from the
// start).
func TestSaveProgressSetsFinishedAtOnce(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	audiobookID, userID := insertTestAudiobook(t, store, 100)

	if err := store.SaveProgress(ctx, audiobookID, userID, 50); err != nil {
		t.Fatalf("save progress (not yet finished): %v", err)
	}
	overview, err := store.GetListeningOverview(ctx, userID, nil)
	if err != nil {
		t.Fatalf("GetListeningOverview: %v", err)
	}
	if len(overview.FinishedBooks) != 0 {
		t.Fatalf("finished books = %+v, want none yet", overview.FinishedBooks)
	}

	if err := store.SaveProgress(ctx, audiobookID, userID, 99); err != nil {
		t.Fatalf("save progress (finished): %v", err)
	}
	overview, err = store.GetListeningOverview(ctx, userID, nil)
	if err != nil {
		t.Fatalf("GetListeningOverview: %v", err)
	}
	if len(overview.FinishedBooks) != 1 {
		t.Fatalf("finished books = %+v, want exactly one", overview.FinishedBooks)
	}
	firstFinishedAt := overview.FinishedBooks[0].FinishedAt

	// Relistening from the start must not clear or move finished_at.
	if err := store.SaveProgress(ctx, audiobookID, userID, 0); err != nil {
		t.Fatalf("save progress (restarted): %v", err)
	}
	overview, err = store.GetListeningOverview(ctx, userID, nil)
	if err != nil {
		t.Fatalf("GetListeningOverview: %v", err)
	}
	if len(overview.FinishedBooks) != 1 || !overview.FinishedBooks[0].FinishedAt.Equal(firstFinishedAt) {
		t.Fatalf("finished books = %+v, want finished_at unchanged at %v", overview.FinishedBooks, firstFinishedAt)
	}
}

// TestGetListeningOverviewRestrictsFinishedBooksByTag covers that a
// tag-restricted account's finished-books list only includes books carrying
// one of its allowed tags - the same restriction ListAudiobooks applies, so
// a restricted account can't see a finished book here that it couldn't
// otherwise see or open.
func TestGetListeningOverviewRestrictsFinishedBooksByTag(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	allowedBookID, userID := insertTestAudiobook(t, store, 100)
	restrictedBookID, _ := insertTestAudiobook(t, store, 100)

	if _, err := store.ReplaceAudiobookTags(ctx, allowedBookID, []string{"allowed"}); err != nil {
		t.Fatalf("tag allowed book: %v", err)
	}
	if _, err := store.ReplaceAudiobookTags(ctx, restrictedBookID, []string{"restricted"}); err != nil {
		t.Fatalf("tag restricted book: %v", err)
	}

	// Both books are finished under the SAME listening user (insertTestAudiobook's
	// returned userID is only each book's uploader, which SaveProgress ignores).
	if err := store.SaveProgress(ctx, allowedBookID, userID, 99); err != nil {
		t.Fatalf("finish allowed book: %v", err)
	}
	if err := store.SaveProgress(ctx, restrictedBookID, userID, 99); err != nil {
		t.Fatalf("finish restricted book: %v", err)
	}

	overview, err := store.GetListeningOverview(ctx, userID, []string{"allowed"})
	if err != nil {
		t.Fatalf("GetListeningOverview: %v", err)
	}
	if len(overview.FinishedBooks) != 1 || overview.FinishedBooks[0].ID != allowedBookID {
		t.Fatalf("finished books = %+v, want only %q", overview.FinishedBooks, allowedBookID)
	}

	unrestricted, err := store.GetListeningOverview(ctx, userID, nil)
	if err != nil {
		t.Fatalf("GetListeningOverview (unrestricted): %v", err)
	}
	if len(unrestricted.FinishedBooks) != 2 {
		t.Fatalf("finished books (unrestricted) = %+v, want both books", unrestricted.FinishedBooks)
	}
}
