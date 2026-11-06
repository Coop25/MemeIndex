package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestBackup lays out a completed backup exactly as installExport does.
func writeTestBackup(t *testing.T, dataDir, id string, createdAt time.Time, partSizes ...int) {
	t.Helper()
	dir := filepath.Join(dataDir, "backups", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := backupPartsManifest{Kind: backupPartsManifestKind, BackupID: id, CreatedAt: createdAt, PartCount: len(partSizes)}
	for i, size := range partSizes {
		name := backupPartFilename(id, i+1)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", size)), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest.Parts = append(manifest.Parts, backupPart{Index: i + 1, Filename: name, SizeBytes: int64(size), SHA256: strings.Repeat("a", 64)})
		manifest.TotalSizeBytes += int64(size)
	}
	if err := writeJSONFile(filepath.Join(dir, backupManifestFilename(id)), manifest); err != nil {
		t.Fatal(err)
	}
}

func TestLastCompletedBackup(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir}
	now := time.Now().UTC()

	got, err := b.lastCompletedBackup(now)
	if err != nil || got != nil {
		t.Fatalf("empty = %+v, %v", got, err)
	}

	writeTestBackup(t, dataDir, "memeindex-backup-20260101-000000", now.Add(-10*24*time.Hour), 5)
	writeTestBackup(t, dataDir, "memeindex-backup-20260102-000000", now.Add(-2*24*time.Hour), 3, 4)
	// Newer directory without a manifest must be ignored.
	if err := os.MkdirAll(filepath.Join(dataDir, "backups", "memeindex-backup-20260103-000000"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err = b.lastCompletedBackup(now)
	if err != nil || got == nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got.SizeBytes != 7 || got.Stale || !got.CompletedAt.Equal(now.Add(-2*24*time.Hour)) {
		t.Fatalf("got %+v", got)
	}
	got, _ = b.lastCompletedBackup(now.Add(9 * 24 * time.Hour))
	if got == nil || !got.Stale {
		t.Fatalf("expected stale, got %+v", got)
	}
}
