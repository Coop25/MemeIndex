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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const portableBackupFormatVersion = 1

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
	{name: "app_users", columns: "user_id, username, display_name, avatar_url, last_active_at, can_view, can_upload, can_add_tags, can_remove_tags, can_delete_memes, created_at, updated_at"},
	{name: "app_user_readd_required", columns: "user_id, blocked_at"},
	{name: "meme_tags", columns: "meme_id, tag_id"},
	{name: "user_favorites", columns: "user_id, meme_id"},
	{name: "reel_sessions", columns: "id, history, position, last_activity"},
	{name: "meme_audit_logs", columns: "id, meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at"},
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

	for _, dir := range []string{"uploads", "thumbnails"} {
		if err := addDirectoryToTar(tw, filepath.Join(b.dataDir, dir), dir); err != nil {
			return "", err
		}
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

func (b *portableBackup) importArchive(ctx context.Context, source io.Reader) error {
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
	for _, dir := range []string{"uploads", "thumbnails"} {
		if info, err := os.Stat(filepath.Join(staging, dir)); err != nil || !info.IsDir() {
			return fmt.Errorf("backup is missing %s directory", dir)
		}
	}
	if err := validateRestoredMemes(staging); err != nil {
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

	if _, err := conn.Exec(ctx, "TRUNCATE TABLE meme_tags, user_favorites, reel_sessions, meme_audit_logs, tags, memes, app_user_readd_required, app_users RESTART IDENTITY CASCADE"); err != nil {
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
		info, err := os.Stat(filepath.Join(staging, "uploads", storedName))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("backup is missing meme file uploads/%s", storedName)
		}
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

	for _, dir := range []string{"uploads", "thumbnails"} {
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
	return rollback, finish, nil
}

func writeTarBytes(tw *tar.Writer, name string, payload []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), ModTime: time.Now().UTC(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(payload)
	return err
}

func addDirectoryToTar(tw *tar.Writer, root, archiveRoot string) error {
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
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("cannot back up non-regular file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := archiveRoot + "/" + filepath.ToSlash(rel)
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
	gz, err := gzip.NewReader(source)
	if err != nil {
		return errors.New("file is not a valid MemeIndex backup")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read backup archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		clean := filepath.ToSlash(filepath.Clean(name))
		allowed := clean == "manifest.json" || strings.HasPrefix(clean, "database/") || clean == "uploads" || strings.HasPrefix(clean, "uploads/") || clean == "thumbnails" || strings.HasPrefix(clean, "thumbnails/")
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
