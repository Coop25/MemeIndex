package client

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
	stopRequested, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 10, true, false)
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
	stopRequested, err = store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 15, true, false)
	if err != nil {
		t.Fatalf("heartbeat A (after stop request): %v", err)
	}
	if !stopRequested {
		t.Fatalf("heartbeat A (after stop request): stopRequested = false, want true")
	}
	// A reported itself playing on that heartbeat, but it's about to pause -
	// it must not reappear as "playing elsewhere" to B in the meantime.
	if _, active, err := store.OtherActivePlaybackSession(ctx, audiobookID, userID, deviceB, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("OtherActivePlaybackSession: %v", err)
	} else if active {
		t.Fatalf("A shows as playing right after being told to stop")
	}

	// ...and clear it, so a subsequent heartbeat does not see it again.
	stopRequested, err = store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 20, true, false)
	if err != nil {
		t.Fatalf("heartbeat A (after clearing): %v", err)
	}
	if stopRequested {
		t.Fatalf("heartbeat A (after clearing): stopRequested = true, want false (flag should have cleared)")
	}
}

// TestStalePlaybackStopRequestDoesNotPauseResumedDevice covers the "pressed
// play and it instantly paused itself" bug: a stop request used to be stamped
// on every other device's row, idle or not, and sat there until that device's
// next heartbeat - which was the one sent the moment the listener pressed play
// on it, so it paused right back.
func TestStalePlaybackStopRequestDoesNotPauseResumedDevice(t *testing.T) {
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
	deviceC := "dev-c-" + uuid.Must(uuid.NewV6()).String()

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

	// A is paused (idle), C is playing. B takes over.
	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 10, false, false); err != nil {
		t.Fatalf("heartbeat A: %v", err)
	}
	if _, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceC, 30, true, false); err != nil {
		t.Fatalf("heartbeat C: %v", err)
	}
	if err := store.RequestOtherPlaybackSessionsStop(ctx, audiobookID, userID, deviceB); err != nil {
		t.Fatalf("RequestOtherPlaybackSessionsStop: %v", err)
	}

	// The idle device was never playing, so it must not have been flagged.
	stopRequested, err := store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceA, 10, true, false)
	if err != nil {
		t.Fatalf("heartbeat A (play): %v", err)
	}
	if stopRequested {
		t.Fatalf("idle device A was told to stop; only playing devices should be")
	}

	// C was flagged but never heard about it before its listener pressed play
	// again on it - that press wins over the older request.
	pending, err := store.OtherPlaybackStopPending(ctx, audiobookID, userID, deviceB, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("OtherPlaybackStopPending: %v", err)
	}
	if !pending {
		t.Fatalf("stop on C should still be pending before C heartbeats")
	}
	stopRequested, err = store.HeartbeatPlaybackSession(ctx, audiobookID, userID, deviceC, 30, true, true)
	if err != nil {
		t.Fatalf("heartbeat C (resumed): %v", err)
	}
	if stopRequested {
		t.Fatalf("resumed heartbeat on C returned stopRequested = true, want false")
	}
	pending, err = store.OtherPlaybackStopPending(ctx, audiobookID, userID, deviceB, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("OtherPlaybackStopPending (after C): %v", err)
	}
	if pending {
		t.Fatalf("stop on C should have cleared once C heartbeated")
	}
}
