package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// backupPartMaxBytes caps each downloadable piece of a backup so a large
// library can be fetched over several sittings instead of one huge transfer
// that has to finish in a single go. A var rather than a const so tests can
// shrink it.
var backupPartMaxBytes int64 = 25 << 30

const (
	backupPartsManifestKind = "memeindex-backup-parts"
	// exportPartsIndexName is the sidecar export() leaves next to its raw
	// part files; installExport turns it into the user-facing manifest.
	exportPartsIndexName = "parts.json"
	maxBackupParts       = 999
)

var backupIDPattern = regexp.MustCompile(`^memeindex-backup-[0-9]{8}-[0-9]{6}(-[0-9]+)?$`)

type backupPart struct {
	Index     int    `json:"index"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// backupPartsManifest is the small file shipped alongside a split backup. It
// says how many parts make up the archive and what each one must look like, so
// a restore can tell when it has every correct piece before touching the
// server.
type backupPartsManifest struct {
	Kind               string       `json:"kind"`
	BackupID           string       `json:"backup_id"`
	CreatedAt          time.Time    `json:"created_at"`
	AppVersion         string       `json:"app_version"`
	PartCount          int          `json:"part_count"`
	TotalSizeBytes     int64        `json:"total_size_bytes"`
	PartSizeLimitBytes int64        `json:"part_size_limit_bytes"`
	Parts              []backupPart `json:"parts"`
	Instructions       string       `json:"instructions"`
}

func backupManifestFilename(backupID string) string {
	return backupID + ".manifest.json"
}

func backupPartFilename(backupID string, index int) string {
	return fmt.Sprintf("%s.tar.gz.part%03d", backupID, index)
}

func exportPartName(index int) string {
	return fmt.Sprintf("part%03d", index)
}

func validateBackupPartsManifest(m backupPartsManifest) error {
	if m.Kind != backupPartsManifestKind {
		return errors.New("file is not a MemeIndex backup manifest")
	}
	if !backupIDPattern.MatchString(m.BackupID) {
		return errors.New("backup manifest has an invalid backup id")
	}
	if m.PartCount < 1 || m.PartCount > maxBackupParts || m.PartCount != len(m.Parts) {
		return errors.New("backup manifest part count does not match its part list")
	}
	var total int64
	for i, part := range m.Parts {
		if part.Index != i+1 || part.Filename != backupPartFilename(m.BackupID, i+1) {
			return fmt.Errorf("backup manifest part %d has an unexpected name", i+1)
		}
		if part.SizeBytes <= 0 {
			return fmt.Errorf("backup manifest part %d has an invalid size", i+1)
		}
		if decoded, err := hex.DecodeString(part.SHA256); err != nil || len(decoded) != sha256.Size || part.SHA256 != strings.ToLower(part.SHA256) {
			return fmt.Errorf("backup manifest part %d has an invalid checksum", i+1)
		}
		total += part.SizeBytes
	}
	if total != m.TotalSizeBytes {
		return errors.New("backup manifest total size does not match its parts")
	}
	return nil
}

func readBackupPartsManifest(path string) (backupPartsManifest, error) {
	var manifest backupPartsManifest
	payload, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid backup manifest: %w", err)
	}
	return manifest, validateBackupPartsManifest(manifest)
}

func writeJSONFile(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

// splitPartWriter spreads one byte stream across numbered part files of at
// most limit bytes each, hashing every part as it goes. Concatenating the
// parts in order reproduces the original stream exactly.
type splitPartWriter struct {
	dir     string
	limit   int64
	file    *os.File
	hash    hash.Hash
	written int64
	parts   []backupPart
}

func (w *splitPartWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if w.file == nil || w.written >= w.limit {
			if err := w.rotate(); err != nil {
				return total, err
			}
		}
		chunk := p
		if room := w.limit - w.written; int64(len(chunk)) > room {
			chunk = chunk[:room]
		}
		n, err := w.file.Write(chunk)
		w.hash.Write(chunk[:n])
		w.written += int64(n)
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (w *splitPartWriter) rotate() error {
	if err := w.finishPart(); err != nil {
		return err
	}
	if len(w.parts) >= maxBackupParts {
		return errors.New("backup exceeds the maximum number of parts")
	}
	index := len(w.parts) + 1
	file, err := os.OpenFile(filepath.Join(w.dir, exportPartName(index)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	w.hash = sha256.New()
	w.written = 0
	w.parts = append(w.parts, backupPart{Index: index, Filename: exportPartName(index)})
	return nil
}

func (w *splitPartWriter) finishPart() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	last := &w.parts[len(w.parts)-1]
	last.SizeBytes = w.written
	last.SHA256 = hex.EncodeToString(w.hash.Sum(nil))
	return err
}

// Close finishes the last part and records the part list for installExport.
func (w *splitPartWriter) Close() error {
	if err := w.finishPart(); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(w.dir, exportPartsIndexName), w.parts)
}

func readExportParts(dir string) ([]backupPart, error) {
	payload, err := os.ReadFile(filepath.Join(dir, exportPartsIndexName))
	if err != nil {
		return nil, fmt.Errorf("read export part list: %w", err)
	}
	var parts []backupPart
	if err := json.Unmarshal(payload, &parts); err != nil {
		return nil, fmt.Errorf("read export part list: %w", err)
	}
	if len(parts) == 0 {
		return nil, errors.New("export produced no archive parts")
	}
	return parts, nil
}

// openPartFiles returns one reader over the given part files in order, i.e.
// the original .tar.gz stream.
func openPartFiles(paths []string) (io.Reader, func(), error) {
	files := make([]*os.File, 0, len(paths))
	closeAll := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	readers := make([]io.Reader, 0, len(paths))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		files = append(files, file)
		readers = append(readers, file)
	}
	return io.MultiReader(readers...), closeAll, nil
}

// --- Restoring from parts -------------------------------------------------
//
// A split backup is restored by staging its manifest, then uploading each
// part (in any order, over as many sittings as needed). Parts are checked
// against the manifest's size and checksum as they arrive and kept on disk,
// so an interrupted upload only has to resend what is missing. Once every
// part is present the restore runs from the concatenated parts.

type backupIncomingPart struct {
	backupPart
	Received bool `json:"received"`
}

type backupIncomingStatus struct {
	Staged         bool                 `json:"staged"`
	BackupID       string               `json:"backup_id,omitempty"`
	CreatedAt      *time.Time           `json:"created_at,omitempty"`
	PartCount      int                  `json:"part_count"`
	ReceivedCount  int                  `json:"received_count"`
	TotalSizeBytes int64                `json:"total_size_bytes"`
	Complete       bool                 `json:"complete"`
	Parts          []backupIncomingPart `json:"parts,omitempty"`
}

func (b *portableBackup) incomingDir() string {
	return filepath.Join(b.backupDir(), "incoming")
}

func (b *portableBackup) incomingManifestPath() string {
	return filepath.Join(b.incomingDir(), "manifest.json")
}

// incomingStatusLocked reports the staged manifest and which parts have
// arrived. Callers hold incomingMu.
func (b *portableBackup) incomingStatusLocked() (backupIncomingStatus, *backupPartsManifest, error) {
	manifest, err := readBackupPartsManifest(b.incomingManifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return backupIncomingStatus{}, nil, nil
	}
	if err != nil {
		return backupIncomingStatus{}, nil, err
	}
	createdAt := manifest.CreatedAt
	status := backupIncomingStatus{
		Staged:         true,
		BackupID:       manifest.BackupID,
		CreatedAt:      &createdAt,
		PartCount:      manifest.PartCount,
		TotalSizeBytes: manifest.TotalSizeBytes,
		Parts:          make([]backupIncomingPart, 0, len(manifest.Parts)),
	}
	for _, part := range manifest.Parts {
		info, err := os.Stat(filepath.Join(b.incomingDir(), part.Filename))
		received := err == nil && info.Mode().IsRegular() && info.Size() == part.SizeBytes
		if received {
			status.ReceivedCount++
		}
		status.Parts = append(status.Parts, backupIncomingPart{backupPart: part, Received: received})
	}
	status.Complete = status.ReceivedCount == status.PartCount
	return status, &manifest, nil
}

func (b *portableBackup) incomingStatus() (backupIncomingStatus, error) {
	b.incomingMu.Lock()
	defer b.incomingMu.Unlock()
	status, _, err := b.incomingStatusLocked()
	return status, err
}

// stageImportManifest records which backup is being restored. Re-sending the
// manifest of the backup already staged keeps the parts received so far;
// a different backup discards them and starts over.
func (b *portableBackup) stageImportManifest(payload []byte) (backupIncomingStatus, error) {
	var manifest backupPartsManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return backupIncomingStatus{}, errors.New("file is not a MemeIndex backup manifest")
	}
	if err := validateBackupPartsManifest(manifest); err != nil {
		return backupIncomingStatus{}, err
	}

	b.incomingMu.Lock()
	defer b.incomingMu.Unlock()
	if current, err := readBackupPartsManifest(b.incomingManifestPath()); err != nil || current.BackupID != manifest.BackupID || !slices.Equal(current.Parts, manifest.Parts) {
		if err := os.RemoveAll(b.incomingDir()); err != nil {
			return backupIncomingStatus{}, err
		}
	}
	if err := os.MkdirAll(b.incomingDir(), 0o755); err != nil {
		return backupIncomingStatus{}, err
	}
	if err := writeJSONFile(b.incomingManifestPath(), manifest); err != nil {
		return backupIncomingStatus{}, err
	}
	status, _, err := b.incomingStatusLocked()
	return status, err
}

// receiveImportPart stores one uploaded part, keeping it only if it matches
// the staged manifest's size and checksum exactly.
func (b *portableBackup) receiveImportPart(filename string, body io.Reader) (backupIncomingStatus, error) {
	b.incomingMu.Lock()
	defer b.incomingMu.Unlock()
	_, manifest, err := b.incomingStatusLocked()
	if err != nil {
		return backupIncomingStatus{}, err
	}
	if manifest == nil {
		return backupIncomingStatus{}, errors.New("upload the backup's .manifest.json file before its parts")
	}
	var expected *backupPart
	for i := range manifest.Parts {
		if manifest.Parts[i].Filename == filename {
			expected = &manifest.Parts[i]
			break
		}
	}
	if expected == nil {
		return backupIncomingStatus{}, fmt.Errorf("%q is not a part of backup %s", filename, manifest.BackupID)
	}

	temp, err := os.CreateTemp(b.incomingDir(), ".uploading-*")
	if err != nil {
		return backupIncomingStatus{}, err
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(body, expected.SizeBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return backupIncomingStatus{}, fmt.Errorf("receive %s: %w", filename, copyErr)
	}
	if closeErr != nil {
		return backupIncomingStatus{}, closeErr
	}
	if written != expected.SizeBytes {
		return backupIncomingStatus{}, fmt.Errorf("%s is %d bytes but the manifest expects %d - the file may be incomplete", filename, written, expected.SizeBytes)
	}
	if hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
		return backupIncomingStatus{}, fmt.Errorf("%s does not match the manifest checksum - the file is damaged or from a different backup", filename)
	}
	if err := os.Rename(tempPath, filepath.Join(b.incomingDir(), filename)); err != nil {
		return backupIncomingStatus{}, err
	}
	keep = true
	status, _, err := b.incomingStatusLocked()
	return status, err
}

func (b *portableBackup) discardIncoming() error {
	b.incomingMu.Lock()
	defer b.incomingMu.Unlock()
	return os.RemoveAll(b.incomingDir())
}

// importStagedParts restores the server from a fully uploaded split backup
// and clears the staging area once the restore succeeds.
func (b *portableBackup) importStagedParts(ctx context.Context) error {
	b.incomingMu.Lock()
	defer b.incomingMu.Unlock()
	status, manifest, err := b.incomingStatusLocked()
	if err != nil {
		return err
	}
	if manifest == nil {
		return errors.New("no backup parts have been uploaded")
	}
	if !status.Complete {
		return fmt.Errorf("backup is incomplete: %d of %d parts received", status.ReceivedCount, status.PartCount)
	}
	paths := make([]string, 0, len(manifest.Parts))
	for _, part := range manifest.Parts {
		paths = append(paths, filepath.Join(b.incomingDir(), part.Filename))
	}
	source, closeParts, err := openPartFiles(paths)
	if err != nil {
		return err
	}
	// Media is already compressed, so a genuine archive expands to roughly
	// its own size; leave generous headroom over the default ceiling.
	err = b.importArchiveWithLimit(ctx, source, defaultMaxExtractBytes+2*manifest.TotalSizeBytes)
	closeParts()
	if err != nil {
		return err
	}
	return os.RemoveAll(b.incomingDir())
}
