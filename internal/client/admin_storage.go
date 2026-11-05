package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// orphanGracePeriod keeps freshly written files out of the orphan list: an
// upload or conversion in flight has a file on disk before its row exists.
const orphanGracePeriod = time.Hour

type storageDirUsage struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

// storageBuckets is the fixed report order of measureStorage.
var storageBuckets = []string{"uploads", "thumbnails", "audiobooks", "audiobooks/covers", "audiobooks/tmp", "downloads", "backups", "other"}

// storageBucket maps a path relative to the data dir (slash separated) to its
// report bucket. Files directly at the top level fall into "other".
func storageBucket(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return "other"
	}
	switch parts[0] {
	case "uploads", "thumbnails", "downloads", "backups":
		return parts[0]
	case "audiobooks":
		if len(parts) > 2 && (parts[1] == "covers" || parts[1] == "tmp") {
			return "audiobooks/" + parts[1]
		}
		return "audiobooks"
	}
	return "other"
}

// measureStorage sums file sizes per data-dir area. Symlinks are neither
// followed nor counted; missing directories measure 0.
func measureStorage(dataDir string) ([]storageDirUsage, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	usage := make(map[string]*storageDirUsage, len(storageBuckets))
	out := make([]storageDirUsage, len(storageBuckets))
	for i, name := range storageBuckets {
		out[i] = storageDirUsage{Name: name, Path: filepath.Join(root, filepath.FromSlash(name))}
		usage[name] = &out[i]
	}
	out[len(out)-1].Path = root

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root || d.IsDir() || !d.Type().IsRegular() {
			return nil // directories, symlinks and special files add nothing
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		u := usage[storageBucket(filepath.ToSlash(rel))]
		u.Bytes += info.Size()
		u.Files++
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, walkErr
	}
	return out, nil
}

// storageReferences holds the absolute, cleaned file paths the database
// references.
type storageReferences struct {
	Paths map[string]struct{}
	// ScanAudiobooks reports whether the audiobook directories may be scanned
	// for orphans. It is false without an audiobook database: with no DB we
	// cannot know which audiobook files are referenced, so every one of them
	// would look orphaned.
	ScanAudiobooks bool
}

type orphanFile struct {
	Path       string    `json:"path"`
	Dir        string    `json:"dir"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}

type missingFile struct {
	Kind  string `json:"kind"` // meme|audiobook|cover
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

func normalizeStoragePath(p string) string {
	if abs, err := filepath.Abs(filepath.Clean(p)); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func pathStem(p string) string {
	return strings.TrimSuffix(p, filepath.Ext(p))
}

// findOrphans lists unreferenced regular files directly inside uploads,
// thumbnails, audiobooks, audiobooks/covers and audiobooks/tmp. It is
// deliberately conservative - when in doubt a file is NOT an orphan. Skipped
// by rule:
//   - anything modified within orphanGracePeriod of now (or in the future)
//   - directories (so no recursion: subfolders such as thumbnails/tagframes
//     hold derived data and are never reported), symlinks, special files
//   - dotfiles
//   - "*.remux-failed" markers (audiobookRemuxFailurePath)
//   - an unreferenced audiobooks/<stem>.<ext> sharing its stem with a
//     referenced audiobook file (a remux's in-flight .m4b next to its mp3)
//   - the audiobook dirs entirely unless refs.ScanAudiobooks
func findOrphans(dataDir string, refs storageReferences, now time.Time) ([]orphanFile, error) {
	root := normalizeStoragePath(dataDir)
	audiobooksDir := filepath.Join(root, "audiobooks")
	referencedStems := map[string]struct{}{}
	for p := range refs.Paths {
		if filepath.Dir(p) == audiobooksDir {
			referencedStems[pathStem(p)] = struct{}{}
		}
	}

	scans := []struct {
		rel        string
		audiobooks bool
	}{
		{"uploads", false}, {"thumbnails", false},
		{"audiobooks", true}, {"audiobooks/covers", true}, {"audiobooks/tmp", true},
	}
	out := []orphanFile{}
	for _, scan := range scans {
		if scan.audiobooks && !refs.ScanAudiobooks {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(scan.rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", scan.rel, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.Type().IsRegular() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".remux-failed") {
				continue
			}
			path := filepath.Join(dir, name)
			if _, ok := refs.Paths[path]; ok {
				continue
			}
			if scan.rel == "audiobooks" {
				if _, ok := referencedStems[pathStem(path)]; ok {
					continue
				}
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) < orphanGracePeriod {
				continue
			}
			out = append(out, orphanFile{Path: path, Dir: scan.rel, Bytes: info.Size(), ModifiedAt: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// collectStorageReferences gathers every file path the databases reference and
// the rows whose file is gone. It fails closed: any error means the caller
// must not delete anything.
func (s *Server) collectStorageReferences(ctx context.Context) (storageReferences, []missingFile, error) {
	refs := storageReferences{Paths: map[string]struct{}{}}
	missing := []missingFile{}
	dataDir := normalizeStoragePath(s.config.DataDir)

	uploadDir := normalizeStoragePath(s.managers.UploadDir())
	thumbDir := filepath.Join(dataDir, "thumbnails")
	if td := strings.TrimSpace(s.managers.ThumbnailDir()); td != "" {
		thumbDir = normalizeStoragePath(td)
	}
	// findOrphans scans <dataDir>/uploads and <dataDir>/thumbnails; if the
	// stores point elsewhere, every file there would look unreferenced.
	if uploadDir != filepath.Join(dataDir, "uploads") || thumbDir != filepath.Join(dataDir, "thumbnails") {
		return refs, nil, fmt.Errorf("meme store directories do not match data dir %s", dataDir)
	}

	files, err := s.managers.StoredMemeFiles()
	if err != nil {
		return refs, nil, err
	}
	for _, f := range files {
		names := []string{}
		for _, raw := range []string{f.StoredName, f.FilePath} {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if base := filepath.Base(raw); base != "." && base != string(filepath.Separator) {
				names = append(names, base)
			}
		}
		exists := false
		for _, name := range names {
			up := filepath.Join(uploadDir, name)
			refs.Paths[up] = struct{}{}
			// Previews are <stem>.jpg in thumbnails (accessor thumbnailFileName).
			refs.Paths[filepath.Join(thumbDir, pathStem(name)+".jpg")] = struct{}{}
			if _, err := os.Stat(up); err == nil {
				exists = true
			}
		}
		if !exists && !f.Hidden && len(names) > 0 {
			missing = append(missing, missingFile{Kind: "meme", ID: f.ID, Title: f.Title, Path: filepath.Join(uploadDir, names[0])})
		}
	}

	// Without an audiobook database nothing audiobook-related is referenced
	// and, crucially, ScanAudiobooks stays false so those dirs are not scanned.
	if s.audiobooks == nil {
		return refs, missing, nil
	}
	books, err := s.audiobooks.ListStoragePaths(ctx)
	if err != nil {
		return storageReferences{Paths: map[string]struct{}{}}, nil, err
	}
	temps, err := s.audiobooks.ListUploadTempPaths(ctx)
	if err != nil {
		return storageReferences{Paths: map[string]struct{}{}}, nil, err
	}
	refs.ScanAudiobooks = true
	// The DB stores absolute paths; also protect the same file name inside the
	// current data dir in case the data dir was moved since they were written.
	add := func(stored, localDir string) (abs string, present bool) {
		stored = strings.TrimSpace(stored)
		if stored == "" {
			return "", true
		}
		abs = normalizeStoragePath(stored)
		local := filepath.Join(localDir, filepath.Base(abs))
		refs.Paths[abs] = struct{}{}
		refs.Paths[local] = struct{}{}
		if _, err := os.Stat(abs); err == nil {
			return abs, true
		}
		_, err := os.Stat(local)
		return abs, err == nil
	}
	audiobooksDir := filepath.Join(dataDir, "audiobooks")
	for _, b := range books {
		if p, ok := add(b.StoragePath, audiobooksDir); !ok {
			missing = append(missing, missingFile{Kind: "audiobook", ID: b.ID, Title: b.Title, Path: p})
		}
		if p, ok := add(b.CoverPath, filepath.Join(audiobooksDir, "covers")); !ok {
			missing = append(missing, missingFile{Kind: "cover", ID: b.ID, Title: b.Title, Path: p})
		}
	}
	for _, t := range temps {
		add(t, filepath.Join(audiobooksDir, "tmp"))
	}
	return refs, missing, nil
}

// storageReportTTL is how long GET /api/admin/storage serves a cached report;
// measuring walks the whole data dir.
const storageReportTTL = 5 * time.Minute

// storageLargestLimit caps the largest-files table.
const storageLargestLimit = 25

// storageDeleteMaxPaths bounds one orphan delete batch.
const storageDeleteMaxPaths = 10000

type storageLargeFile struct {
	Kind  string `json:"kind"` // meme|audiobook
	ID    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Owner string `json:"owner"`
}

type adminUploadInProgress struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	UserID        string    `json:"user_id"`
	DisplayName   string    `json:"display_name"`
	ReceivedBytes int64     `json:"received_bytes"`
	TotalBytes    int64     `json:"total_bytes"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type adminStorageReport struct {
	Dirs        []storageDirUsage       `json:"dirs"`
	Disk        *adminDiskStats         `json:"disk"`
	Largest     []storageLargeFile      `json:"largest"`
	Uploads     []adminUploadInProgress `json:"uploads"`
	GeneratedAt time.Time               `json:"generated_at"`
}

// adminStorageState lives on Server. The report is a cache; scan is the
// allow-list for orphan deletes: only a path the last successful scan
// reported may ever be removed.
type adminStorageState struct {
	reportMu    sync.Mutex
	report      adminStorageReport
	reportAt    time.Time
	reportValid bool

	scanMu sync.Mutex
	scan   map[string]orphanFile // nil until a scan succeeds

	// deleteMu runs orphan delete batches one at a time.
	deleteMu sync.Mutex
}

func (st *adminStorageState) invalidateReport() {
	st.reportMu.Lock()
	st.reportValid = false
	st.reportMu.Unlock()
}

// setScan replaces the allow-list; nil clears it (nothing deletable).
func (st *adminStorageState) setScan(orphans []orphanFile) {
	var allow map[string]orphanFile
	if orphans != nil {
		allow = make(map[string]orphanFile, len(orphans))
		for _, o := range orphans {
			allow[o.Path] = o
		}
	}
	st.scanMu.Lock()
	st.scan = allow
	st.scanMu.Unlock()
}

// scanAllowed reports whether path was in the last successful scan.
func (st *adminStorageState) scanAllowed(path string) (allowed, scanned bool) {
	st.scanMu.Lock()
	defer st.scanMu.Unlock()
	if st.scan == nil {
		return false, false
	}
	_, ok := st.scan[path]
	return ok, true
}

func (st *adminStorageState) forgetScanned(path string) {
	st.scanMu.Lock()
	delete(st.scan, path)
	st.scanMu.Unlock()
}

func (s *Server) handleAdminStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	report, err := s.cachedStorageReport(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		log.Printf("admin storage report failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage report failed"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) cachedStorageReport(ctx context.Context, refresh bool) (adminStorageReport, error) {
	st := &s.storage
	st.reportMu.Lock()
	defer st.reportMu.Unlock()
	if !refresh && st.reportValid && time.Since(st.reportAt) < storageReportTTL {
		return st.report, nil
	}
	report, err := s.buildStorageReport(ctx)
	if err != nil {
		return adminStorageReport{}, err
	}
	st.report, st.reportAt, st.reportValid = report, time.Now(), true
	return report, nil
}

func (s *Server) buildStorageReport(ctx context.Context) (adminStorageReport, error) {
	report := adminStorageReport{Largest: []storageLargeFile{}, Uploads: []adminUploadInProgress{}, GeneratedAt: time.Now().UTC()}
	dirs, err := measureStorage(s.config.DataDir)
	if err != nil {
		return report, err
	}
	report.Dirs = dirs
	if disk, ok := diskUsage(s.config.DataDir); ok {
		report.Disk = &disk
	}
	names := s.adminUserNames(ctx)
	owner := func(userID string) string {
		if name := names[userID]; name != "" {
			return name
		}
		return userID
	}

	if files, err := s.managers.StoredMemeFiles(); err != nil {
		log.Printf("admin storage meme sizes failed: %v", err)
	} else {
		for _, f := range files {
			report.Largest = append(report.Largest, storageLargeFile{Kind: "meme", ID: f.ID, Name: f.Title, Bytes: f.SizeBytes})
		}
	}
	if s.audiobooks != nil {
		if books, err := s.audiobooks.ListStoragePaths(ctx); err != nil {
			log.Printf("admin storage audiobook sizes failed: %v", err)
		} else {
			for _, b := range books {
				report.Largest = append(report.Largest, storageLargeFile{Kind: "audiobook", ID: b.ID, Name: b.Title, Bytes: b.SizeBytes, Owner: owner(b.UploadedBy)})
			}
		}
		if uploads, err := s.audiobooks.ListInProgressUploads(ctx); err != nil {
			log.Printf("admin storage uploads failed: %v", err)
		} else {
			for _, u := range uploads {
				report.Uploads = append(report.Uploads, adminUploadInProgress{
					ID: u.ID, Title: u.Title, UserID: u.UserID, DisplayName: owner(u.UserID),
					ReceivedBytes: u.ReceivedBytes, TotalBytes: u.TotalBytes, UpdatedAt: u.UpdatedAt,
				})
			}
		}
	}
	sort.SliceStable(report.Largest, func(i, j int) bool {
		if report.Largest[i].Bytes != report.Largest[j].Bytes {
			return report.Largest[i].Bytes > report.Largest[j].Bytes
		}
		return report.Largest[i].ID < report.Largest[j].ID
	})
	if len(report.Largest) > storageLargestLimit {
		report.Largest = report.Largest[:storageLargestLimit]
	}
	return report, nil
}

// adminUserNames maps user IDs to display names (falling back to username).
func (s *Server) adminUserNames(ctx context.Context) map[string]string {
	names := map[string]string{}
	if s.users == nil {
		return names
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		log.Printf("admin storage user names failed: %v", err)
		return names
	}
	for _, user := range users {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = strings.TrimSpace(user.Username)
		}
		names[user.UserID] = name
	}
	return names
}

func (s *Server) handleStorageOrphanScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	refs, missing, err := s.collectStorageReferences(r.Context())
	var orphans []orphanFile
	if err == nil {
		orphans, err = findOrphans(s.config.DataDir, refs, time.Now())
	}
	if err != nil {
		// A failed scan clears the allow-list: nothing is deletable until a
		// scan succeeds again.
		s.storage.setScan(nil)
		log.Printf("admin storage orphan scan failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage scan failed"})
		return
	}
	s.storage.setScan(orphans)
	writeJSON(w, http.StatusOK, map[string]any{"orphans": orphans, "missing": missing, "scanned_at": time.Now().UTC()})
}

type orphanDeleteResult struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
	Error   string `json:"error"`
}

// handleStorageOrphanDelete is the only place files are removed by path.
// The whole batch is refused (409) while a backup or restore holds the
// backup lock, and (500) if references cannot be re-collected. Each path is
// then checked by checkOrphanDeletable and removed with os.Remove (never
// RemoveAll).
func (s *Server) handleStorageOrphanDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(body.Paths) > storageDeleteMaxPaths {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("at most %d paths per request", storageDeleteMaxPaths)})
		return
	}

	s.storage.deleteMu.Lock()
	defer s.storage.deleteMu.Unlock()
	// A backup reads, and a restore replaces, the files being deleted.
	if s.backup != nil {
		if !s.backup.mu.TryLock() {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "backup or restore in progress"})
			return
		}
		defer s.backup.mu.Unlock()
	}

	// Re-check under the lock: a file referenced (or rewritten) since the
	// scan must not be deleted.
	refs, _, err := s.collectStorageReferences(r.Context())
	var fresh []orphanFile
	if err == nil {
		fresh, err = findOrphans(s.config.DataDir, refs, time.Now())
	}
	if err != nil {
		log.Printf("admin storage orphan delete: reference check failed, nothing deleted: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not verify file references; nothing was deleted"})
		return
	}
	freshSet := make(map[string]struct{}, len(fresh))
	for _, o := range fresh {
		freshSet[o.Path] = struct{}{}
	}
	root := normalizeStoragePath(s.config.DataDir)
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)

	results := make([]orphanDeleteResult, 0, len(body.Paths))
	seen := make(map[string]struct{}, len(body.Paths))
	var deleted int
	var freed int64
	for _, raw := range body.Paths {
		res := orphanDeleteResult{Path: raw}
		path, size, reason := s.checkOrphanDeletable(raw, root, resolvedRoot, rootErr, freshSet, seen)
		if reason != "" {
			res.Error = reason
		} else if err := os.Remove(path); err != nil {
			res.Error = "remove failed: " + err.Error()
		} else {
			res.Deleted = true
			deleted++
			freed += size
			s.storage.forgetScanned(path)
		}
		results = append(results, res)
	}
	if deleted > 0 {
		s.storage.invalidateReport()
	}
	log.Printf("storage cleanup by %s: %d files, %d bytes", storageActorName(r), deleted, freed)
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "bytes_freed": freed})
}

// checkOrphanDeletable applies every per-path guard, in order, and returns
// the cleaned path and its size, or a non-empty refusal reason:
//  1. non-empty, absolute, filepath.Clean'ed, not repeated in the batch
//  2. in the last successful scan's orphan set (the allow-list)
//  3. in the fresh orphan set computed under the backup lock from freshly
//     collected references (still unreferenced, past the grace period, a
//     regular file directly inside a scanned dir)
//  4. inside the data dir: filepath.Rel(dataDir, p) does not start with ".."
//  5. its parent directory, symlinks resolved, is exactly the expected one
//     under the resolved data dir (no symlinked directory leads elsewhere)
//  6. Lstat says a regular file (not a symlink, directory or special file)
func (s *Server) checkOrphanDeletable(raw, root, resolvedRoot string, rootErr error, fresh, seen map[string]struct{}) (string, int64, string) {
	if strings.TrimSpace(raw) == "" {
		return "", 0, "empty path"
	}
	if !filepath.IsAbs(raw) {
		return "", 0, "path must be absolute"
	}
	path := filepath.Clean(raw)
	if _, dup := seen[path]; dup {
		return "", 0, "duplicate path in request"
	}
	seen[path] = struct{}{}
	allowed, scanned := s.storage.scanAllowed(path)
	if !scanned {
		return "", 0, "no orphan scan has been run"
	}
	if !allowed {
		return "", 0, "not in the last orphan scan"
	}
	if _, ok := fresh[path]; !ok {
		return "", 0, "no longer an orphan (referenced, recently modified, or gone)"
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return "", 0, "outside the data directory"
	}
	if rootErr != nil {
		return "", 0, "cannot resolve the data directory"
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || parent != filepath.Join(resolvedRoot, filepath.Dir(rel)) {
		return "", 0, "parent directory resolves outside its expected location"
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, "cannot stat file"
	}
	if !info.Mode().IsRegular() {
		return "", 0, "not a regular file"
	}
	return path, info.Size(), ""
}

func storageActorName(r *http.Request) string {
	actor := currentAuditActor(r)
	for _, name := range []string{actor.Username, actor.DisplayName, actor.UserID} {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return "unknown"
}

// handleAdminAudiobookUpload serves DELETE /api/admin/audiobooks/uploads/{id}:
// an admin discarding any user's in-progress upload.
func (s *Server) handleAdminAudiobookUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/audiobooks/uploads/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if s.audiobooks == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
		return
	}
	upload, err := s.audiobooks.GetUploadByID(r.Context(), id)
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "upload not found"})
		return
	}
	if err != nil {
		log.Printf("admin audiobook upload lookup failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload lookup failed"})
		return
	}
	if err := s.discardAudiobookUpload(r.Context(), upload); err != nil {
		log.Printf("admin audiobook upload discard failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "discard failed"})
		return
	}
	s.storage.invalidateReport()
	log.Printf("audiobooks: upload %s discarded by %s (%d/%d bytes)", upload.ID, storageActorName(r), upload.ReceivedBytes, upload.TotalBytes)
	w.WriteHeader(http.StatusNoContent)
}
