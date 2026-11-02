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
	"strings"
	"testing"
	"time"
)

func TestPortableBackupJobPersistsCompletedArchive(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	b.exportFunc = func(context.Context) (string, error) {
		return writeTestExportDir(dataDir, "portable backup", 1<<20)
	}

	if status, started := b.startExport(); !started || status.State != "running" {
		t.Fatalf("expected running backup job, got started=%v status=%+v", started, status)
	}
	status := waitForBackupStatus(t, b, "ready")
	if !status.DownloadAvailable || status.Filename == "" || status.SizeBytes == 0 || len(status.Parts) != 1 {
		t.Fatalf("expected downloadable completed backup, got %+v", status)
	}

	file, openedStatus, err := b.openLatestExport(status.Parts[0].Filename)
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
	if restoredStatus.State != "ready" || restoredStatus.Filename != status.Filename || !restoredStatus.DownloadAvailable || len(restoredStatus.Parts) != 1 {
		t.Fatalf("expected completed backup to survive restart, got %+v", restoredStatus)
	}
}

// writeTestExportDir stands in for export(): it splits payload into parts of
// at most limit bytes the same way a real export does.
func writeTestExportDir(dataDir, payload string, limit int64) (string, error) {
	dir, err := os.MkdirTemp(dataDir, ".test-export-*")
	if err != nil {
		return "", err
	}
	w := &splitPartWriter{dir: dir, limit: limit}
	if _, err := w.Write([]byte(payload)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return dir, nil
}

func TestPortableBackupSplitsIntoPartsWithManifest(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	payload := strings.Repeat("0123456789", 5) + "tail" // 54 bytes
	exportDir, err := writeTestExportDir(dataDir, payload, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.installExport(exportDir); err != nil {
		t.Fatalf("install export: %v", err)
	}
	status := b.exportStatus()
	if len(status.Parts) != 3 || status.SizeBytes != int64(len(payload)) {
		t.Fatalf("expected 3 parts totalling %d bytes, got %+v", len(payload), status)
	}

	manifest, err := readBackupPartsManifest(filepath.Join(b.latestDir, status.Filename))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.PartCount != 3 || !strings.HasSuffix(manifest.Parts[2].Filename, ".tar.gz.part003") {
		t.Fatalf("unexpected manifest %+v", manifest)
	}

	var rebuilt strings.Builder
	for _, part := range manifest.Parts {
		file, _, err := b.openLatestExport(part.Filename)
		if err != nil {
			t.Fatalf("open %s: %v", part.Filename, err)
		}
		chunk, _ := io.ReadAll(file)
		_ = file.Close()
		if int64(len(chunk)) > 20 {
			t.Fatalf("part %s is %d bytes, over the limit", part.Filename, len(chunk))
		}
		rebuilt.Write(chunk)
	}
	if rebuilt.String() != payload {
		t.Fatalf("concatenated parts = %q, want %q", rebuilt.String(), payload)
	}

	for _, name := range []string{"", "../" + status.Filename, exportPartsIndexName, "manifest.json"} {
		if _, _, err := b.openLatestExport(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("openLatestExport(%q) err = %v, want ErrNotExist", name, err)
		}
	}
}

func TestPortableBackupRestartSkipsIncompleteBackup(t *testing.T) {
	dataDir := t.TempDir()
	b := &portableBackup{dataDir: dataDir, job: backupJobStatus{State: "idle"}}
	exportDir, err := writeTestExportDir(dataDir, "abcdefghij", 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.installExport(exportDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(b.latestDir, b.exportStatus().Parts[1].Filename)); err != nil {
		t.Fatal(err)
	}
	if status := newPortableBackup("postgres://unused", dataDir).exportStatus(); status.DownloadAvailable {
		t.Fatalf("backup with a missing part should not be offered, got %+v", status)
	}
}

func TestBackupImportPartsStageVerifyAndAssemble(t *testing.T) {
	sourceDir := t.TempDir()
	source := &portableBackup{dataDir: sourceDir, job: backupJobStatus{State: "idle"}}
	payload := "the original archive bytes, split three ways"
	exportDir, err := writeTestExportDir(sourceDir, payload, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.installExport(exportDir); err != nil {
		t.Fatal(err)
	}
	status := source.exportStatus()
	manifestBytes, err := os.ReadFile(filepath.Join(source.latestDir, status.Filename))
	if err != nil {
		t.Fatal(err)
	}
	partBytes := func(i int) []byte {
		data, err := os.ReadFile(filepath.Join(source.latestDir, status.Parts[i].Filename))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	dest := &portableBackup{dataDir: t.TempDir()}
	if _, err := dest.receiveImportPart(status.Parts[0].Filename, bytes.NewReader(partBytes(0))); err == nil {
		t.Fatal("part accepted before any manifest was staged")
	}
	if _, err := dest.stageImportManifest([]byte(`{"kind":"something-else"}`)); err == nil {
		t.Fatal("accepted a manifest of the wrong kind")
	}
	staged, err := dest.stageImportManifest(manifestBytes)
	if err != nil || !staged.Staged || staged.PartCount != 3 || staged.ReceivedCount != 0 {
		t.Fatalf("stage manifest: status=%+v err=%v", staged, err)
	}

	// Out of order, with a corrupt and a truncated attempt rejected.
	corrupt := partBytes(2)
	corrupt[0] ^= 0xff
	if _, err := dest.receiveImportPart(status.Parts[2].Filename, bytes.NewReader(corrupt)); err == nil {
		t.Fatal("accepted a part with a bad checksum")
	}
	if _, err := dest.receiveImportPart(status.Parts[0].Filename, bytes.NewReader(partBytes(0)[:5])); err == nil {
		t.Fatal("accepted a truncated part")
	}
	if _, err := dest.receiveImportPart("../escape", bytes.NewReader(partBytes(0))); err == nil {
		t.Fatal("accepted a part name outside the manifest")
	}
	if _, err := dest.receiveImportPart(status.Parts[2].Filename, bytes.NewReader(partBytes(2))); err != nil {
		t.Fatalf("receive part 3: %v", err)
	}
	if err := dest.importStagedParts(context.Background()); err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("expected incomplete-backup error, got %v", err)
	}

	// Re-staging the same manifest (e.g. a later sitting) keeps what arrived.
	staged, err = dest.stageImportManifest(manifestBytes)
	if err != nil || staged.ReceivedCount != 1 || !staged.Parts[2].Received {
		t.Fatalf("restaging same manifest lost received parts: %+v err=%v", staged, err)
	}
	for _, i := range []int{0, 1} {
		staged, err = dest.receiveImportPart(status.Parts[i].Filename, bytes.NewReader(partBytes(i)))
		if err != nil {
			t.Fatalf("receive part %d: %v", i+1, err)
		}
	}
	if !staged.Complete {
		t.Fatalf("expected complete staging, got %+v", staged)
	}

	paths := make([]string, 0, 3)
	for _, part := range status.Parts {
		paths = append(paths, filepath.Join(dest.incomingDir(), part.Filename))
	}
	reader, closeParts, err := openPartFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	assembled, _ := io.ReadAll(reader)
	closeParts()
	if string(assembled) != payload {
		t.Fatalf("assembled staged parts = %q, want %q", assembled, payload)
	}

	if err := dest.discardIncoming(); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := dest.incomingStatus(); cleared.Staged {
		t.Fatalf("expected staging cleared, got %+v", cleared)
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
		return writeTestExportDir(dataDir, "first backup", 1<<20)
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
		"manifest.json":          `{"format_version":1}`,
		"database/memes.csv":     "id,name\n",
		"uploads/example.webp":   "meme",
		"audiobooks/example.mp3": "audiobook",
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
	payload, err = os.ReadFile(filepath.Join(destination, "audiobooks", "example.mp3"))
	if err != nil {
		t.Fatalf("read extracted audiobook: %v", err)
	}
	if string(payload) != "audiobook" {
		t.Fatalf("unexpected extracted payload %q", payload)
	}
}

func TestSwapMediaDirectoriesCanRollback(t *testing.T) {
	dataDir := t.TempDir()
	staging := t.TempDir()
	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
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
	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		if _, err := os.Stat(filepath.Join(dataDir, dir, "old")); err != nil {
			t.Fatalf("old %s was not restored: %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, dir, "new")); !os.IsNotExist(err) {
			t.Fatalf("new %s remained after rollback: %v", dir, err)
		}
	}
}

func TestSwapMediaDirectoriesRecreatesAudiobooksTmp(t *testing.T) {
	dataDir := t.TempDir()
	staging := t.TempDir()
	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	backup := &portableBackup{dataDir: dataDir}
	_, finish, err := backup.swapMediaDirectories(staging)
	if err != nil {
		t.Fatalf("swap media: %v", err)
	}
	finish()
	info, err := os.Stat(filepath.Join(dataDir, "audiobooks", "tmp"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected audiobooks/tmp to be recreated after restore: %v", err)
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
	columns := strings.Split(portableBackupTables[0].columns, ", ")
	values := make([]string, len(columns))
	values[0], values[2] = "abc", "abc.webp"
	values[3] = "/uploads/abc.webp"
	csv := strings.Join(columns, ",") + "\n" + strings.Join(values, ",") + "\n"
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

func TestValidateRestoredAudiobooksRequiresEveryFile(t *testing.T) {
	dataDir := t.TempDir()
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, "database"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "audiobooks"), 0o755); err != nil {
		t.Fatal(err)
	}

	columns := strings.Split(backupTableColumns("audiobooks"), ", ")
	storagePathColumn, coverPathColumn := -1, -1
	for i, name := range columns {
		switch name {
		case "storage_path":
			storagePathColumn = i
		case "cover_path":
			coverPathColumn = i
		}
	}
	storagePath := filepath.Join(dataDir, "audiobooks", "book1.mp3")
	values := make([]string, len(columns))
	values[storagePathColumn] = storagePath
	values[coverPathColumn] = ""
	writeAudiobooksCSV := func() {
		csv := strings.Join(columns, ",") + "\n" + strings.Join(values, ",") + "\n"
		if err := os.WriteFile(filepath.Join(staging, "database", "audiobooks.csv"), []byte(csv), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeAudiobooksCSV()

	if err := validateRestoredAudiobooks(staging, dataDir); err == nil {
		t.Fatal("expected a missing audiobook file to reject the backup")
	}
	if err := os.WriteFile(filepath.Join(staging, "audiobooks", "book1.mp3"), []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoredAudiobooks(staging, dataDir); err != nil {
		t.Fatalf("expected complete backup to validate: %v", err)
	}

	// A backup made under a different MEMEINDEX_DATA_DIR stores an absolute
	// storage_path that cannot possibly fall under this server's own
	// audiobooks directory - must be rejected with a clear error instead of
	// restoring a dangling reference.
	values[storagePathColumn] = filepath.Join(t.TempDir(), "audiobooks", "book1.mp3")
	writeAudiobooksCSV()
	if err := validateRestoredAudiobooks(staging, dataDir); err == nil {
		t.Fatal("expected a storage_path outside this server's data directory to be rejected")
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

// The parts of a split export must reassemble into an archive the normal
// restore extractor accepts, even when part boundaries fall mid-entry.
func TestSplitArchivePartsExtractAsOneArchive(t *testing.T) {
	dir := t.TempDir()
	split := &splitPartWriter{dir: dir, limit: 97}
	gz := gzip.NewWriter(split)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"manifest.json":         `{"format_version":3}`,
		"database/memes.csv":    strings.Repeat("row,", 200),
		"uploads/a.png":         strings.Repeat("\x89PNG", 300),
		"audiobooks/book/1.m4b": strings.Repeat("audio", 400),
	}
	for name, payload := range files {
		if err := writeTarBytes(tw, name, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := split.Close(); err != nil {
		t.Fatal(err)
	}
	parts, err := readExportParts(dir)
	if err != nil || len(parts) < 3 {
		t.Fatalf("expected several parts, got %d (err %v)", len(parts), err)
	}
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, filepath.Join(dir, part.Filename))
	}
	reader, closeParts, err := openPartFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer closeParts()
	dest := t.TempDir()
	if err := extractPortableArchive(reader, dest); err != nil {
		t.Fatalf("extract reassembled parts: %v", err)
	}
	for name, payload := range files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || string(got) != payload {
			t.Fatalf("%s did not survive the split (err %v)", name, err)
		}
	}
}
