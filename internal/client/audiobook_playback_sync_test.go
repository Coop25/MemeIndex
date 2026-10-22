package client

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestHeartbeatPlaybackSessionClearsStopRequested is a regression test for a
// real Postgres bug: HeartbeatPlaybackSession used to read stop_requested and
// clear it via two separate writable CTEs targeting the same row in one
// statement, which Postgres does not guarantee to apply reliably. The
// practical symptom was "pause there, play here" never actually pausing the
// other device - RequestOtherPlaybackSessionsStop set stop_requested, but the
// target device's next heartbeat silently failed to see or clear it.
//
// It is skipped unless MEMEINDEX_TEST_DATABASE_URL points at a disposable
// Postgres (see internal/accessor/postgres_store_integration_test.go for how
// to run one locally).
func TestHeartbeatPlaybackSessionClearsStopRequested(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}

	ctx := context.Background()
	st, err := newAudiobookStore(ctx, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("newAudiobookStore: %v", err)
	}
	store := st.(*postgresAudiobookStore)
	t.Cleanup(store.pool.Close)

	audiobookID := uuid.Must(uuid.NewV6()).String()
	userID := "u-" + uuid.Must(uuid.NewV6()).String()
	deviceA := "dev-a-" + uuid.Must(uuid.NewV6()).String()
	deviceB := "dev-b-" + uuid.Must(uuid.NewV6()).String()

	_, err = store.pool.Exec(ctx, `
		INSERT INTO audiobooks (id, title, filename, size_bytes, storage_path, uploaded_by_user_id)
		VALUES ($1, 'Test Book', 'test.mp3', 1, '/tmp/test.mp3', $2)
	`, audiobookID, userID)
	if err != nil {
		t.Fatalf("insert test audiobook: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM audiobooks WHERE id = $1`, audiobookID)
	})

	// Device A establishes a playing session.
	stopRequested, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 10, true)
	if err != nil {
		t.Fatalf("heartbeat A (initial): %v", err)
	}
	if stopRequested {
		t.Fatalf("heartbeat A (initial): stopRequested = true, want false")
	}

	// Device B asks every other device (A) to stop.
	if err := store.RequestOtherPlaybackSessionsStop(ctx, audiobookID, userID, deviceB); err != nil {
		t.Fatalf("RequestOtherPlaybackSessionsStop: %v", err)
	}

	// Device A's next heartbeat must observe the stop request...
	stopRequested, err = store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 15, true)
	if err != nil {
		t.Fatalf("heartbeat A (after stop request): %v", err)
	}
	if !stopRequested {
		t.Fatalf("heartbeat A (after stop request): stopRequested = false, want true")
	}

	// ...and clear it, so a subsequent heartbeat does not see it again.
	stopRequested, err = store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 20, true)
	if err != nil {
		t.Fatalf("heartbeat A (after clearing): %v", err)
	}
	if stopRequested {
		t.Fatalf("heartbeat A (after clearing): stopRequested = true, want false (flag should have cleared)")
	}
}
