package client

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Bumped from 1 to 2 when audiobooks (files, metadata, bookmarks, series,
// favorites, progress) were added to the backup. An older format-1 archive
// has no audiobooks.csv or audiobooks/ directory at all, so it is rejected
// by format-version mismatch below rather than failing confusingly partway
// through restore.
const portableBackupFormatVersion = 2

type portableBackup struct {
	databaseURL string
	dataDir     string
	mu          sync.Mutex
	jobMu       sync.RWMutex
	job         backupJobStatus
	latestPath  string
	exportFunc  func(context.Context) (string, error)
}

type backupJobStatus struct {
	State             string     `json:"state"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	Filename          string     `json:"filename,omitempty"`
	SizeBytes         int64      `json:"size_bytes,omitempty"`
	DownloadAvailable bool       `json:"download_available"`
	Error             string     `json:"error,omitempty"`
}

type portableBackupManifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
}

type backupTable struct {
	name    string
	columns string
}

var portableBackupTables = []backupTable{
	{name: "memes", columns: "id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, hidden_from_app, pending_delete, delete_requested_by_user_id, delete_requested_at, created_at, updated_at, suggested_tags, auto_suggest_disabled"},
	{name: "tags", columns: "id, name"},
	{name: "app_users", columns: "user_id, username, display_name, avatar_url, last_active_at, can_view, can_upload, can_add_tags, can_remove_tags, can_delete_memes, created_at, updated_at, can_view_audiobooks, can_upload_audiobooks, can_delete_audiobooks, can_manage_audiobook_series"},
	{name: "app_user_readd_required", columns: "user_id, blocked_at"},
	{name: "meme_tags", columns: "meme_id, tag_id"},
	{name: "user_favorites", columns: "user_id, meme_id"},
	{name: "reel_sessions", columns: "id, history, position, last_activity"},
	{name: "meme_audit_logs", columns: "id, meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at"},
	// audiobook_series must precede audiobooks (series_id FK), and audiobooks
	// must precede its own dependents (audiobook_id FK, ON DELETE CASCADE) -
	// restore applies this list in order via COPY FROM, so the order here is
	// load-bearing, not cosmetic. audiobook_uploads (in-progress/resumable
	// upload tracking) is deliberately excluded: its rows reference a
	// temp_path scratch file that is never part of the backup, the same way
	// data/downloads link-import staging never is - an in-progress upload is
	// just lost, same as it would be across a restart today.
	{name: "audiobook_series", columns: "id, name, created_by_user_id, created_at"},
	{name: "audiobooks", columns: "id, title, author, narrator, filename, mime_type, size_bytes, duration_seconds, storage_path, cover_path, chapters, uploaded_by_user_id, created_at, series_id, series_sequence"},
	{name: "audiobook_bookmarks", columns: "id, audiobook_id, user_id, position_seconds, label, created_at"},
	{name: "audiobook_favorites", columns: "audiobook_id, user_id, created_at"},
	{name: "audiobook_progress", columns: "audiobook_id, user_id, position_seconds, updated_at"},
	{name: "audiobook_playback_sessions", columns: "audiobook_id, user_id, device_id, position_seconds, is_playing, stop_requested, updated_at"},
}

// backupTableColumns looks up a table's export/import column list by name,
// for callers (like validateRestoredAudiobooks) that need it without relying
// on a fixed index into portableBackupTables.
func backupTableColumns(name string) string {
	for _, table := range portableBackupTables {
		if table.name == name {
			return table.columns
		}
	}
	return ""
}

func newPortableBackup(databaseURL, dataDir string) *portableBackup {
	if strings.TrimSpace(databaseURL) == "" {
		return nil
	}
	b := &portableBackup{
		databaseURL: databaseURL,
		dataDir:     dataDir,
		job:         backupJobStatus{State: "idle"},
	}
	b.exportFunc = b.export
	b.restoreLatestExport()
	return b
}

func (b *portableBackup) backupDir() string {
	return filepath.Join(b.dataDir, "backups")
}

func (b *portableBackup) restoreLatestExport() {
	entries, err := os.ReadDir(b.backupDir())
	if err != nil {
		return
	}

	var latestPath string
	var latestInfo os.FileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "memeindex-backup-") || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if latestInfo == nil || info.ModTime().After(latestInfo.ModTime()) {
			latestPath = filepath.Join(b.backupDir(), entry.Name())
			latestInfo = info
		}
	}
	if latestInfo == nil {
		return
	}

	completedAt := latestInfo.ModTime().UTC()
	b.latestPath = latestPath
	b.job = backupJobStatus{
		State:             "ready",
		CompletedAt:       &completedAt,
		Filename:          filepath.Base(latestPath),
		SizeBytes:         latestInfo.Size(),
		DownloadAvailable: true,
	}
}

func (b *portableBackup) exportStatus() backupJobStatus {
	b.jobMu.RLock()
	defer b.jobMu.RUnlock()
	return b.job
}

func (b *portableBackup) startExport() (backupJobStatus, bool) {
	b.jobMu.Lock()
	if b.job.State == "running" {
		status := b.job
		b.jobMu.Unlock()
		return status, false
	}

	startedAt := time.Now().UTC()
	b.job.State = "running"
	b.job.StartedAt = &startedAt
	b.job.Error = ""
	status := b.job
	b.jobMu.Unlock()

	go b.runExportJob()
	return status, true
}

func (b *portableBackup) runExportJob() {
	exportFunc := b.exportFunc
	if exportFunc == nil {
		exportFunc = b.export
	}
	archivePath, err := exportFunc(context.Background())
	if err != nil {
		log.Printf("backup export task failed: %v", err)
		b.failExportJob(err)
		return
	}
	if err := b.installExport(archivePath); err != nil {
		_ = os.Remove(archivePath)
		log.Printf("backup export task failed while storing archive: %v", err)
		b.failExportJob(err)
		return
	}
	log.Printf("backup export task completed: %s", b.exportStatus().Filename)
}

func (b *portableBackup) failExportJob(err error) {
	logMessage := strings.TrimSpace(err.Error())
	if logMessage == "" {
		logMessage = "backup creation failed"
	}
	b.jobMu.Lock()
	b.job.State = "failed"
	b.job.Error = logMessage
	b.jobMu.Unlock()
}

func (b *portableBackup) installExport(archivePath string) error {
	if err := os.MkdirAll(b.backupDir(), 0o755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}

	completedAt := time.Now().UTC()
	filename := "memeindex-backup-" + completedAt.Format("20060102-150405") + ".tar.gz"
	destination := filepath.Join(b.backupDir(), filename)
	if _, err := os.Stat(destination); err == nil {
		filename = "memeindex-backup-" + completedAt.Format("20060102-150405") + fmt.Sprintf("-%d.tar.gz", completedAt.UnixNano())
		destination = filepath.Join(b.backupDir(), filename)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check backup destination: %w", err)
	}
	if err := os.Rename(archivePath, destination); err != nil {
		return fmt.Errorf("store completed backup: %w", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		return fmt.Errorf("inspect completed backup: %w", err)
	}

	b.jobMu.Lock()
	previousPath := b.latestPath
	b.latestPath = destination
	b.job = backupJobStatus{
		State:             "ready",
		StartedAt:         b.job.StartedAt,
		CompletedAt:       &completedAt,
		Filename:          filename,
		SizeBytes:         info.Size(),
		DownloadAvailable: true,
	}
	b.jobMu.Unlock()

	if previousPath != "" && previousPath != destination {
		_ = os.Remove(previousPath)
	}
	return nil
}

func (b *portableBackup) openLatestExport() (*os.File, backupJobStatus, error) {
	b.jobMu.RLock()
	status := b.job
	path := b.latestPath
	if path == "" || !status.DownloadAvailable {
		b.jobMu.RUnlock()
		return nil, status, os.ErrNotExist
	}
	file, err := os.Open(path)
	b.jobMu.RUnlock()
	if err != nil {
		return nil, status, err
	}
	return file, status, nil
}

func (b *portableBackup) export(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		return "", err
	}
	output, err := os.CreateTemp(b.dataDir, ".memeindex-export-*.tar.gz")
	if err != nil {
		return "", err
	}
	outputPath := output.Name()
	keep := false
	defer func() {
		_ = output.Close()
		if !keep {
			_ = os.Remove(outputPath)
		}
	}()

	pool, err := pgxpool.New(ctx, b.databaseURL)
	if err != nil {
		return "", fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire database connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return "", fmt.Errorf("begin database snapshot: %w", err)
	}
	defer conn.Exec(context.Background(), "ROLLBACK")

	gz := gzip.NewWriter(output)
	tw := tar.NewWriter(gz)
	closeArchive := func() error {
		if err := tw.Close(); err != nil {
			return err
		}
		if err := gz.Close(); err != nil {
			return err
		}
		return output.Close()
	}

	manifest, err := json.MarshalIndent(portableBackupManifest{
		FormatVersion: portableBackupFormatVersion,
		CreatedAt:     time.Now().UTC(),
		AppVersion:    BuildVersion(),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	manifest = append(manifest, '\n')
	if err := writeTarBytes(tw, "manifest.json", manifest); err != nil {
		return "", err
	}

	for _, table := range portableBackupTables {
		name := "database/" + table.name + ".csv"
		header := &tar.Header{Name: name, Mode: 0o600, ModTime: time.Now().UTC(), Typeflag: tar.TypeReg}
		// PostgreSQL COPY needs a seek-free writer, while tar requires the size first.
		csvFile, err := os.CreateTemp(b.dataDir, ".memeindex-table-*.csv")
		if err != nil {
			return "", err
		}
		csvPath := csvFile.Name()
		_, copyErr := conn.Conn().PgConn().CopyTo(ctx, csvFile, fmt.Sprintf("COPY (SELECT %s FROM %s) TO STDOUT WITH (FORMAT CSV, HEADER TRUE)", table.columns, table.name))
		closeErr := csvFile.Close()
		if copyErr != nil {
			_ = os.Remove(csvPath)
			return "", fmt.Errorf("export table %s: %w", table.name, copyErr)
		}
		if closeErr != nil {
			_ = os.Remove(csvPath)
			return "", closeErr
		}
		info, err := os.Stat(csvPath)
		if err != nil {
			_ = os.Remove(csvPath)
			return "", err
		}
		header.Size = info.Size()
		if err := tw.WriteHeader(header); err != nil {
			_ = os.Remove(csvPath)
			return "", err
		}
		csvFile, err = os.Open(csvPath)
		if err == nil {
			_, err = io.Copy(tw, csvFile)
			_ = csvFile.Close()
		}
		_ = os.Remove(csvPath)
		if err != nil {
			return "", err
		}
	}
	rows, err := conn.Query(ctx, "SELECT stored_name FROM memes")
	if err != nil {
		return "", fmt.Errorf("verify snapshot uploads: %w", err)
	}
	for rows.Next() {
		var storedName string
		if err := rows.Scan(&storedName); err != nil {
			rows.Close()
			return "", err
		}
		if !validStoredName(storedName) {
			rows.Close()
			return "", fmt.Errorf("database contains invalid stored file name %q", storedName)
		}
		if info, err := os.Stat(filepath.Join(b.dataDir, "uploads", storedName)); err != nil || !info.Mode().IsRegular() {
			rows.Close()
			return "", fmt.Errorf("meme file %q is missing from uploads", storedName)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()

	// Unlike memes.stored_name (a bare file name resolved against dataDir at
	// read time), audiobooks.storage_path/cover_path are stored as absolute
	// filesystem paths (see audiobook_store.go), so verify both that the file
	// exists and that it still lives under this server's own audiobooks
	// directory before it goes in the archive.
	audiobooksDir := filepath.Join(b.dataDir, "audiobooks")
	abRows, err := conn.Query(ctx, "SELECT storage_path, cover_path FROM audiobooks")
	if err != nil {
		return "", fmt.Errorf("verify snapshot audiobooks: %w", err)
	}
	for abRows.Next() {
		var storagePath, coverPath string
		if err := abRows.Scan(&storagePath, &coverPath); err != nil {
			abRows.Close()
			return "", err
		}
		if err := requireAudiobookFileUnder(audiobooksDir, storagePath); err != nil {
			abRows.Close()
			return "", err
		}
		if coverPath != "" {
			if err := requireAudiobookFileUnder(audiobooksDir, coverPath); err != nil {
				abRows.Close()
				return "", err
			}
		}
	}
	if err := abRows.Err(); err != nil {
		abRows.Close()
		return "", err
	}
	abRows.Close()

	for _, dir := range []string{"uploads", "thumbnails"} {
		if err := addDirectoryToTar(tw, filepath.Join(b.dataDir, dir), dir); err != nil {
			return "", err
		}
	}
	// audiobooks/tmp holds in-progress/resumable upload scratch files, which
	// are excluded from the backup for the same reason audiobook_uploads rows
	// are (see portableBackupTables above) - exclude it here too so a backup
	// never carries half-written uploads.
	if err := addDirectoryToTar(tw, audiobooksDir, "audiobooks", "tmp"); err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, "COMMIT"); err != nil {
		return "", fmt.Errorf("finish database snapshot: %w", err)
	}
	if err := closeArchive(); err != nil {
		return "", err
	}
	keep = true
	return outputPath, nil
}

// preRestoreDir holds automatic snapshots taken immediately before an import.
// It is a sibling of backupDir so it never shadows the user-facing "latest
// backup" that restoreLatestExport scans for.
func (b *portableBackup) preRestoreDir() string {
	return filepath.Join(b.backupDir(), "pre-restore")
}

// snapshotBeforeRestore builds a full archive of the current server and stores it
// under preRestoreDir, keeping only the most recent few. It returns the path to
// the snapshot it wrote.
func (b *portableBackup) snapshotBeforeRestore(ctx context.Context) (string, error) {
	exportFunc := b.exportFunc
	if exportFunc == nil {
		exportFunc = b.export
	}
	archivePath, err := exportFunc(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(b.preRestoreDir(), 0o755); err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}
	dest := filepath.Join(b.preRestoreDir(), "memeindex-pre-restore-"+time.Now().UTC().Format("20060102-150405")+".tar.gz")
	if _, err := os.Stat(dest); err == nil {
		dest = filepath.Join(b.preRestoreDir(), fmt.Sprintf("memeindex-pre-restore-%d.tar.gz", time.Now().UTC().UnixNano()))
	}
	if err := os.Rename(archivePath, dest); err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}
	b.pruneOldPreRestoreSnapshots(3)
	return dest, nil
}

// pruneOldPreRestoreSnapshots keeps the newest keep snapshots and removes the rest.
func (b *portableBackup) pruneOldPreRestoreSnapshots(keep int) {
	entries, err := os.ReadDir(b.preRestoreDir())
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "memeindex-pre-restore-") || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) <= keep {
		return
	}
	slices.Sort(names) // timestamped names sort chronologically
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(b.preRestoreDir(), name))
	}
}

func (b *portableBackup) importArchive(ctx context.Context, source io.Reader) error {
	// Snapshot the current server before we replace it so a mistaken restore can be
	// rolled back. Best-effort: a snapshot failure is logged loudly but must not
	// block a deliberate restore. Runs before b.mu is taken because the export
	// path locks b.mu itself.
	if snapshotPath, err := b.snapshotBeforeRestore(ctx); err != nil {
		log.Printf("WARNING: could not snapshot server before restore; proceeding without an undo point: %v", err)
	} else {
		log.Printf("pre-restore snapshot saved to %s", snapshotPath)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	staging, err := os.MkdirTemp(b.dataDir, ".memeindex-import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := extractPortableArchive(source, staging); err != nil {
		return err
	}

	manifestBytes, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return errors.New("backup is missing manifest.json")
	}
	var manifest portableBackupManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("invalid backup manifest: %w", err)
	}
	if manifest.FormatVersion != portableBackupFormatVersion {
		return fmt.Errorf("unsupported backup format version %d", manifest.FormatVersion)
	}
	for _, table := range portableBackupTables {
		if _, err := os.Stat(filepath.Join(staging, "database", table.name+".csv")); err != nil {
			return fmt.Errorf("backup is missing database/%s.csv", table.name)
		}
	}
	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		if info, err := os.Stat(filepath.Join(staging, dir)); err != nil || !info.IsDir() {
			return fmt.Errorf("backup is missing %s directory", dir)
		}
	}
	if err := validateRestoredMemes(staging); err != nil {
		return err
	}
	if err := validateRestoredAudiobooks(staging, b.dataDir); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, b.databaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire database connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.Exec(context.Background(), "ROLLBACK")
		}
	}()

	if _, err := conn.Exec(ctx, "TRUNCATE TABLE meme_tags, user_favorites, reel_sessions, meme_audit_logs, tags, memes, app_user_readd_required, app_users, audiobook_bookmarks, audiobook_favorites, audiobook_progress, audiobook_playback_sessions, audiobooks, audiobook_series RESTART IDENTITY CASCADE"); err != nil {
		return fmt.Errorf("clear current database: %w", err)
	}
	for _, table := range portableBackupTables {
		csvFile, err := os.Open(filepath.Join(staging, "database", table.name+".csv"))
		if err != nil {
			return err
		}
		_, copyErr := conn.Conn().PgConn().CopyFrom(ctx, csvFile, fmt.Sprintf("COPY %s (%s) FROM STDIN WITH (FORMAT CSV, HEADER TRUE)", table.name, table.columns))
		_ = csvFile.Close()
		if copyErr != nil {
			return fmt.Errorf("restore table %s: %w", table.name, copyErr)
		}
	}
	for _, table := range []string{"tags", "meme_audit_logs"} {
		query := fmt.Sprintf("SELECT setval(pg_get_serial_sequence('%s','id'), GREATEST(COALESCE((SELECT MAX(id) FROM %s), 0), 1), (SELECT COUNT(*) > 0 FROM %s))", table, table, table)
		if _, err := conn.Exec(ctx, query); err != nil {
			return fmt.Errorf("restore %s sequence: %w", table, err)
		}
	}

	rollbackFiles, finishFiles, err := b.swapMediaDirectories(staging)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "COMMIT"); err != nil {
		rollbackFiles()
		return fmt.Errorf("commit restored database: %w", err)
	}
	committed = true
	finishFiles()
	return nil
}

func validateRestoredMemes(staging string) error {
	file, err := os.Open(filepath.Join(staging, "database", "memes.csv"))
	if err != nil {
		return err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read memes CSV header: %w", err)
	}
	// COPY imports by position, not the header labels. Validate the same schema
	// before checking stored_name so a reordered header cannot bypass the check.
	expected := strings.Split(portableBackupTables[0].columns, ", ")
	if len(header) != len(expected) {
		return errors.New("memes CSV schema does not match backup format")
	}
	for i := range expected {
		if header[i] != expected[i] {
			return errors.New("memes CSV column order does not match backup format")
		}
	}
	storedNameColumn := -1
	for index, name := range header {
		if name == "stored_name" {
			storedNameColumn = index
			break
		}
	}
	if storedNameColumn < 0 {
		return errors.New("memes CSV is missing stored_name column")
	}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read memes CSV: %w", err)
		}
		if storedNameColumn >= len(record) || !validStoredName(record[storedNameColumn]) {
			return errors.New("memes CSV contains an invalid stored file name")
		}
		storedName := record[storedNameColumn]
		if record[3] != "/uploads/"+storedName {
			return errors.New("memes CSV contains a noncanonical media path")
		}
		if source := strings.TrimSpace(record[8]); source != "" {
			parsed, err := url.Parse(source)
			if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return errors.New("memes CSV contains an unsafe source URL")
			}
		}
		info, err := os.Stat(filepath.Join(staging, "uploads", storedName))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("backup is missing meme file uploads/%s", storedName)
		}
	}
	return nil
}

// validateRestoredAudiobooks confirms every audiobook (and cover, if set)
// referenced by the restored database has a matching file in the backup, and
// that its recorded absolute path still falls under this server's own
// audiobooks directory. Audiobooks store an absolute filesystem path rather
// than a bare file name the way memes do (see audiobook_store.go), so a
// backup taken under a different MEMEINDEX_DATA_DIR would otherwise restore
// database rows pointing at files that can never exist here.
func validateRestoredAudiobooks(staging, dataDir string) error {
	file, err := os.Open(filepath.Join(staging, "database", "audiobooks.csv"))
	if err != nil {
		return err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read audiobooks CSV header: %w", err)
	}
	expected := strings.Split(backupTableColumns("audiobooks"), ", ")
	if len(header) != len(expected) {
		return errors.New("audiobooks CSV schema does not match backup format")
	}
	for i := range expected {
		if header[i] != expected[i] {
			return errors.New("audiobooks CSV column order does not match backup format")
		}
	}
	storagePathColumn, coverPathColumn := -1, -1
	for index, name := range header {
		switch name {
		case "storage_path":
			storagePathColumn = index
		case "cover_path":
			coverPathColumn = index
		}
	}
	if storagePathColumn < 0 || coverPathColumn < 0 {
		return errors.New("audiobooks CSV is missing storage_path or cover_path column")
	}
	audiobooksDir := filepath.Join(dataDir, "audiobooks")
	stagedAudiobooksDir := filepath.Join(staging, "audiobooks")
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read audiobooks CSV: %w", err)
		}
		if storagePathColumn >= len(record) {
			return errors.New("audiobooks CSV row is missing storage_path")
		}
		if err := requireStagedAudiobookFile(audiobooksDir, stagedAudiobooksDir, record[storagePathColumn]); err != nil {
			return err
		}
		if coverPath := strings.TrimSpace(record[coverPathColumn]); coverPath != "" {
			if err := requireStagedAudiobookFile(audiobooksDir, stagedAudiobooksDir, coverPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// requireAudiobookFileUnder confirms path is a regular, existing file located
// under dir - used before a row goes into the archive on export.
func requireAudiobookFileUnder(dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("audiobook file %q is not under the audiobooks directory", path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("audiobook file %q is missing", path)
	}
	return nil
}

// requireStagedAudiobookFile is requireAudiobookFileUnder's restore-side
// counterpart: storagePath is this server's own (not yet applied) absolute
// path from the restored CSV, which it maps onto the equivalent path inside
// the extracted backup to confirm the file actually came along.
func requireStagedAudiobookFile(audiobooksDir, stagedAudiobooksDir, storagePath string) error {
	rel, err := filepath.Rel(audiobooksDir, storagePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("audiobook file %q does not belong under this server's data directory - the backup may be from a server with a different MEMEINDEX_DATA_DIR", storagePath)
	}
	info, err := os.Stat(filepath.Join(stagedAudiobooksDir, rel))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("backup is missing audiobook file audiobooks/%s", filepath.ToSlash(rel))
	}
	return nil
}

func validStoredName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func (b *portableBackup) swapMediaDirectories(staging string) (rollback func(), finish func(), err error) {
	type swap struct{ current, old string }
	swaps := make([]swap, 0, 2)
	rollback = func() {
		for i := len(swaps) - 1; i >= 0; i-- {
			_ = os.RemoveAll(swaps[i].current)
			if swaps[i].old != "" {
				_ = os.Rename(swaps[i].old, swaps[i].current)
			}
		}
	}
	finish = func() {
		for _, item := range swaps {
			if item.old != "" {
				_ = os.RemoveAll(item.old)
			}
		}
	}

	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		current := filepath.Join(b.dataDir, dir)
		incoming := filepath.Join(staging, dir)
		old := ""
		if _, statErr := os.Stat(current); statErr == nil {
			old = filepath.Join(b.dataDir, fmt.Sprintf(".%s-before-import-%d", dir, time.Now().UnixNano()))
			if err = os.Rename(current, old); err != nil {
				rollback()
				return nil, nil, fmt.Errorf("stage current %s: %w", dir, err)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			rollback()
			return nil, nil, statErr
		}
		if err = os.Rename(incoming, current); err != nil {
			if old != "" {
				_ = os.Rename(old, current)
			}
			rollback()
			return nil, nil, fmt.Errorf("install restored %s: %w", dir, err)
		}
		swaps = append(swaps, swap{current: current, old: old})
	}
	// The backup never includes audiobooks/tmp (in-progress upload scratch
	// space, excluded as ephemeral - see export()), so recreate it now. The
	// already-running audiobook store holds this exact path from startup and
	// has no reason to re-create it later, so without this, every upload
	// would fail with ENOENT until the app restarts.
	if err = os.MkdirAll(filepath.Join(b.dataDir, "audiobooks", "tmp"), 0o755); err != nil {
		rollback()
		return nil, nil, fmt.Errorf("recreate audiobooks tmp directory: %w", err)
	}
	return rollback, finish, nil
}

func writeTarBytes(tw *tar.Writer, name string, payload []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), ModTime: time.Now().UTC(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(payload)
	return err
}

func addDirectoryToTar(tw *tar.Writer, root, archiveRoot string, exclude ...string) error {
	if err := tw.WriteHeader(&tar.Header{Name: archiveRoot + "/", Mode: 0o755, ModTime: time.Now().UTC(), Typeflag: tar.TypeDir}); err != nil {
		return err
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return err
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		for _, skip := range exclude {
			if relSlash == skip || strings.HasPrefix(relSlash, skip+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("cannot back up non-regular file %s", path)
		}
		name := archiveRoot + "/" + relSlash
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractPortableArchive(source io.Reader, destination string) error {
	return extractPortableArchiveWithLimits(source, destination, 64<<30, 1000000)
}

func extractPortableArchiveWithLimits(source io.Reader, destination string, maxBytes int64, maxEntries int) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return errors.New("file is not a valid MemeIndex backup")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var expanded int64
	entries := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read backup archive: %w", err)
		}
		entries++
		if entries > maxEntries || header.Size < 0 || header.Size > maxBytes-expanded {
			return errors.New("backup exceeds extraction limits")
		}
		expanded += header.Size
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		clean := filepath.ToSlash(filepath.Clean(name))
		allowed := clean == "manifest.json" || strings.HasPrefix(clean, "database/") || clean == "uploads" || strings.HasPrefix(clean, "uploads/") || clean == "thumbnails" || strings.HasPrefix(clean, "thumbnails/") || clean == "audiobooks" || strings.HasPrefix(clean, "audiobooks/")
		if name == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(name) || !allowed {
			return fmt.Errorf("backup contains invalid path %q", header.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		if target != destination && !strings.HasPrefix(target, destination+string(os.PathSeparator)) {
			return fmt.Errorf("backup path escapes destination: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("extract %s: %w", clean, err)
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("backup contains unsupported entry %q", header.Name)
		}
	}
	return nil
}
