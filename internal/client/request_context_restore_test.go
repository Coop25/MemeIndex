package client

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/audiobooks"
)

// TestRestoreCommitIsNotCancelled cancels a restore while its COMMIT is in
// flight. By then the media directories have been swapped, and a failed
// commit swaps them back; a commit cut short could still have been applied
// and leave the restored database pointing at the old media. Once sent, the
// commit must finish.
func TestRestoreCommitIsNotCancelled(t *testing.T) {
	databaseURL := newScratchBackupDatabase(t)
	ctx := context.Background()
	dataDir := t.TempDir()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataDir, "uploads", "backed-up.png"), []byte("in the backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_users (user_id, session_version) VALUES ('in-backup', 1)`); err != nil {
		t.Fatal(err)
	}

	backup := newPortableBackup(databaseURL, dataDir)
	exportDir, err := backup.export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer os.RemoveAll(exportDir)
	parts, err := readExportParts(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, filepath.Join(exportDir, part.Filename))
	}

	// Change the server after the backup, so the test can tell which state
	// the restore left behind.
	if _, err := pool.Exec(ctx, `INSERT INTO app_users (user_id, session_version) VALUES ('after-backup', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dataDir, "uploads", "backed-up.png")); err != nil {
		t.Fatal(err)
	}
	// Make the commit take a second: a deferred constraint trigger on the
	// restored app_users rows runs pg_sleep when the transaction commits.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION slow_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(1); RETURN NULL; END $$;
		CREATE CONSTRAINT TRIGGER slow_commit AFTER INSERT ON app_users
			DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION slow_commit();
	`); err != nil {
		t.Fatalf("install slow-commit trigger: %v", err)
	}

	reader, closeParts, err := openPartFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer closeParts()

	// Cancel once the media swap has happened: the swap moves the current
	// audiobooks directory aside last, right before the commit is sent.
	restoreCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancelledDuringCommit atomic.Bool
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if matches, _ := filepath.Glob(filepath.Join(dataDir, ".audiobooks-before-import-*")); len(matches) > 0 {
				time.Sleep(200 * time.Millisecond) // well inside the one-second commit
				cancelledDuringCommit.Store(true)
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	err = backup.restoreArchive(restoreCtx, reader, defaultMaxExtractBytes, nil)
	if !cancelledDuringCommit.Load() {
		t.Fatalf("the restore finished before it could be cancelled during its commit (err %v)", err)
	}
	if err != nil {
		t.Fatalf("restore cancelled during its commit failed: %v", err)
	}

	var users []string
	rows, err := pool.Query(ctx, `SELECT user_id FROM app_users ORDER BY user_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		users = append(users, id)
	}
	rows.Close()
	if len(users) != 1 || users[0] != "in-backup" {
		t.Fatalf("users after the restore = %v, want only the backed-up one", users)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "uploads", "backed-up.png")); err != nil {
		t.Fatalf("restored media missing after the restore committed: %v", err)
	}
}

// remuxRecordStore fails RecordRemuxedM4B if its context has ended, as
// Postgres would, and otherwise records that it was called.
type remuxRecordStore struct {
	audiobooks.Store
	recorded atomic.Bool
}

func (s *remuxRecordStore) RecordRemuxedM4B(ctx context.Context, _ audiobooks.StoredMP3, _ string, _ int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.recorded.Store(true)
	return nil
}

// endsWhenFileExists reports itself cancelled once path exists, without
// closing Done, so the ffmpeg run that writes the file is not interrupted:
// as if shutdown arrived just as the conversion finished.
type endsWhenFileExists struct {
	context.Context
	path string
}

func (c endsWhenFileExists) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		return context.Canceled
	}
	return nil
}

// TestRemuxResultIsRecordedWhenShutdownArrivesWithIt checks that a finished
// conversion is recorded even if the job's context ends as it finishes,
// rather than having its .m4b deleted while the update may have landed.
func TestRemuxResultIsRecordedWhenShutdownArrivesWithIt(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	mp3 := filepath.Join(dir, "book.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "anoisesrc=d=2:a=0.3", "-c:a", "libmp3lame", mp3).CombinedOutput(); err != nil {
		t.Fatalf("make test mp3: %v: %s", err, out)
	}
	store := &remuxRecordStore{}
	s := &Server{audiobooks: store}
	ctx := endsWhenFileExists{Context: context.Background(), path: remuxedM4BPath(mp3)}

	if _, err := s.swapAudiobookToRemuxedM4B(ctx, audiobooks.StoredMP3{ID: "book-1", StoragePath: mp3, MimeType: "audio/mpeg", Filename: "book.mp3"}); err != nil {
		t.Fatalf("remux whose context ended as it finished: %v", err)
	}
	if !store.recorded.Load() {
		t.Fatal("the finished remux was not recorded")
	}
	if _, err := os.Stat(remuxedM4BPath(mp3)); err != nil {
		t.Fatalf("the .m4b was removed: %v", err)
	}
}
