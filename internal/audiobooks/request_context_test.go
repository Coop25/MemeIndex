package audiobooks

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestFileWritesFinishDespiteCancelledContext covers the audiobook writes
// that touch a file and its row together (#78). Each one must run to
// completion even when the request that started it has already been
// cancelled, or it would strand a file or lose an upload.
func TestFileWritesFinishDespiteCancelledContext(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	st, err := NewPostgresStore(context.Background(), dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	store := st.(*PostgresStore)
	t.Cleanup(store.Close)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	userID := "u-ctx-" + uuid.NewString()
	// An ID3 header is enough for the content sniff to call it audio.
	audio := []byte("ID3\x04\x00\x00\x00\x00\x00\x00" + strings.Repeat("\x00", 512))

	newUpload := func(t *testing.T, title string) Upload {
		t.Helper()
		upload, err := store.CreateUpload(context.Background(), UploadSeed{
			UserID: userID, Title: title, Filename: title + ".mp3", MimeType: "audio/mpeg", TotalBytes: int64(len(audio)),
		})
		if err != nil {
			t.Fatalf("CreateUpload: %v", err)
		}
		if err := os.WriteFile(upload.TempPath, audio, 0o644); err != nil {
			t.Fatalf("write upload bytes: %v", err)
		}
		if _, err := store.RecordUploadRange(context.Background(), upload.ID, userID, 0, int64(len(audio))); err != nil {
			t.Fatalf("RecordUploadRange: %v", err)
		}
		return upload
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	t.Run("complete upload", func(t *testing.T) {
		upload := newUpload(t, "ctx complete "+uuid.NewString())
		record, err := store.CompleteUpload(cancelled, upload.ID, userID, 1)
		if err != nil {
			t.Fatalf("CompleteUpload with a cancelled context: %v", err)
		}
		t.Cleanup(func() {
			_, _ = store.DeleteAudiobook(context.Background(), record.ID)
		})
		stored, err := store.GetAudiobook(context.Background(), record.ID)
		if err != nil {
			t.Fatalf("completed audiobook not stored: %v", err)
		}
		if !exists(stored.StoragePath) {
			t.Fatalf("completed audiobook's file %s is missing", stored.StoragePath)
		}
		if _, err := store.GetUploadByID(context.Background(), upload.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatalf("upload row left behind after completing: err = %v", err)
		}

		t.Run("set and remove cover", func(t *testing.T) {
			jpeg := []byte("\xff\xd8\xff\xe0 not really a jpeg")
			withCover, err := store.SetAudiobookCover(cancelled, record.ID, jpeg)
			if err != nil {
				t.Fatalf("SetAudiobookCover with a cancelled context: %v", err)
			}
			if withCover.CoverPath == "" || !exists(withCover.CoverPath) {
				t.Fatalf("cover not stored: cover_path %q", withCover.CoverPath)
			}
			cleared, err := store.RemoveAudiobookCover(cancelled, record.ID)
			if err != nil {
				t.Fatalf("RemoveAudiobookCover with a cancelled context: %v", err)
			}
			if cleared.CoverPath != "" || exists(withCover.CoverPath) {
				t.Fatalf("cover not removed: cover_path %q, file still there: %v", cleared.CoverPath, exists(withCover.CoverPath))
			}
		})
	})

	t.Run("abort upload", func(t *testing.T) {
		upload := newUpload(t, "ctx abort "+uuid.NewString())
		if _, err := store.AbortUpload(cancelled, upload.ID, userID); err != nil {
			t.Fatalf("AbortUpload with a cancelled context: %v", err)
		}
		if _, err := store.GetUploadByID(context.Background(), upload.ID); !errors.Is(err, ErrUploadNotFound) {
			t.Fatalf("aborted upload row still present: err = %v", err)
		}
		if exists(upload.TempPath) {
			t.Fatalf("aborted upload's temp file %s still on disk", upload.TempPath)
		}
	})

	t.Run("reads still cancel", func(t *testing.T) {
		if _, err := store.ListAudiobooks(cancelled, userID, ListFilter{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("ListAudiobooks with a cancelled context: err = %v, want context.Canceled", err)
		}
	})
}
