package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPortableBackupJobPersistsCompletedArchive(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	b.exportFunc = func(context.Context) (string, error) {
		file, err := os.CreateTemp(dataDir, ".test-export-*.tar.gz")
		if err != nil {
			return "", err
		}
		if _, err := file.Write([]byte("portable backup")); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return file.Name(), nil
	}

	if status, started := b.startExport(); !started || status.State != "running" {
		t.Fatalf("expected running backup job, got started=%v status=%+v", started, status)
	}
	status := waitForBackupStatus(t, b, "ready")
	if !status.DownloadAvailable || status.Filename == "" || status.SizeBytes == 0 {
		t.Fatalf("expected downloadable completed backup, got %+v", status)
	}

	file, openedStatus, err := b.openLatestExport()
	if err != nil {
		t.Fatalf("open latest export: %v", err)
	}
	payload, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		t.Fatalf("read latest export: %v", err)
	}
	if string(payload) != "portable backup" || openedStatus.Filename != status.Filename {
		t.Fatalf("unexpected persisted export %q with status %+v", payload, openedStatus)
	}

	restarted := newPortableBackup("postgres://unused", dataDir)
	restoredStatus := restarted.exportStatus()
	if restoredStatus.State != "ready" || restoredStatus.Filename != status.Filename || !restoredStatus.DownloadAvailable {
		t.Fatalf("expected completed backup to survive restart, got %+v", restoredStatus)
	}
}

func TestPortableBackupJobRejectsConcurrentStart(t *testing.T) {
	dataDir := t.TempDir()
	release := make(chan struct{})
	entered := make(chan struct{})
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	b.exportFunc = func(context.Context) (string, error) {
		close(entered)
		<-release
		return "", errors.New("stopped for test")
	}

	if _, started := b.startExport(); !started {
		t.Fatal("expected first backup job to start")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("backup job did not start")
	}
	if status, started := b.startExport(); started || status.State != "running" {
		t.Fatalf("expected concurrent start to return running job, got started=%v status=%+v", started, status)
	}
	close(release)
	waitForBackupStatus(t, b, "failed")
}

func TestPortableBackupFailureKeepsPreviousDownload(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	b.exportFunc = func(context.Context) (string, error) {
		file, err := os.CreateTemp(dataDir, ".test-export-*.tar.gz")
		if err != nil {
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return file.Name(), nil
	}
	b.startExport()
	ready := waitForBackupStatus(t, b, "ready")

	b.exportFunc = func(context.Context) (string, error) {
		return "", errors.New("database snapshot failed")
	}
	if _, started := b.startExport(); !started {
		t.Fatal("expected replacement backup to start")
	}
	failed := waitForBackupStatus(t, b, "failed")
	if !failed.DownloadAvailable || failed.Filename != ready.Filename {
		t.Fatalf("expected previous backup to remain downloadable, got %+v", failed)
	}
}

func waitForBackupStatus(t *testing.T, b *portableBackup, expected string) backupJobStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := b.exportStatus()
		if status.State == expected {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backup did not reach %q; last status: %+v", expected, b.exportStatus())
	return backupJobStatus{}
}

func TestExtractPortableArchiveRejectsPathTraversal(t *testing.T) {
	archive := makeTestArchive(t, map[string]string{
		"manifest.json": "{}",
		"../outside":    "nope",
	})
	destination := t.TempDir()
	err := extractPortableArchive(bytes.NewReader(archive), destination)
	if err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(destination, "..", "outside")); !os.IsNotExist(statErr) {
		t.Fatalf("path traversal created a file outside destination: %v", statErr)
	}
}

func TestExtractPortableArchiveAllowsExpectedContents(t *testing.T) {
	archive := makeTestArchive(t, map[string]string{
		"manifest.json":        `{"format_version":1}`,
		"database/memes.csv":   "id,name\n",
		"uploads/example.webp": "meme",
	})
	destination := t.TempDir()
	if err := extractPortableArchive(bytes.NewReader(archive), destination); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(destination, "uploads", "example.webp"))
	if err != nil {
		t.Fatalf("read extracted upload: %v", err)
	}
	if string(payload) != "meme" {
		t.Fatalf("unexpected extracted payload %q", payload)
	}
}

func TestSwapMediaDirectoriesCanRollback(t *testing.T) {
	dataDir := t.TempDir()
	staging := t.TempDir()
	for _, dir := range []string{"uploads", "thumbnails"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, dir, "old"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, dir, "new"), []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	backup := &portableBackup{dataDir: dataDir}
	rollback, _, err := backup.swapMediaDirectories(staging)
	if err != nil {
		t.Fatalf("swap media: %v", err)
	}
	rollback()
	for _, dir := range []string{"uploads", "thumbnails"} {
		if _, err := os.Stat(filepath.Join(dataDir, dir, "old")); err != nil {
			t.Fatalf("old %s was not restored: %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, dir, "new")); !os.IsNotExist(err) {
			t.Fatalf("new %s remained after rollback: %v", dir, err)
		}
	}
}

func TestValidateRestoredMemesRequiresEveryUpload(t *testing.T) {
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, "database"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	csv := "id,stored_name\nabc,abc.webp\n"
	if err := os.WriteFile(filepath.Join(staging, "database", "memes.csv"), []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoredMemes(staging); err == nil {
		t.Fatal("expected a missing upload to reject the backup")
	}
	if err := os.WriteFile(filepath.Join(staging, "uploads", "abc.webp"), []byte("meme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoredMemes(staging); err != nil {
		t.Fatalf("expected complete backup to validate: %v", err)
	}
}

func TestValidStoredNameRejectsPaths(t *testing.T) {
	for _, name := range []string{"../meme.webp", "folder/meme.webp", `folder\\meme.webp`, "", "."} {
		if validStoredName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
	if !validStoredName("meme.webp") {
		t.Fatal("expected a plain stored file name to be accepted")
	}
}

func makeTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for name, payload := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
