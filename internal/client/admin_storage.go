package client

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
