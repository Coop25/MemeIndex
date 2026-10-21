package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/accessor"
	"memeindex/internal/dbschema"
)

// ErrAudiobookUploadNotFound is returned when an upload session id does not
// exist, has already been finalized/aborted, or belongs to a different user.
var ErrAudiobookUploadNotFound = errors.New("audiobook upload not found")

// ErrAudiobookNotFound is returned when a finished audiobook id does not exist.
var ErrAudiobookNotFound = errors.New("audiobook not found")

// ErrAudiobookBookmarkNotFound is returned when a bookmark id does not exist
// or belongs to a different user.
var ErrAudiobookBookmarkNotFound = errors.New("audiobook bookmark not found")

// ErrAudiobookSeriesNotFound is returned when a series id does not exist.
var ErrAudiobookSeriesNotFound = errors.New("audiobook series not found")

// ErrAudiobookSeriesNameTaken is returned when creating or renaming a series
// to a name (case-insensitive) that another series already has.
var ErrAudiobookSeriesNameTaken = errors.New("audiobook series name already in use")

// ErrAudiobookDuplicateTitle is returned by CompleteUpload when the library
// already has an audiobook with the same title (case-insensitive).
var ErrAudiobookDuplicateTitle = errors.New("an audiobook with this title already exists")

type audiobookRecord struct {
	ID               string
	Title            string
	Author           string
	Narrator         string
	Filename         string
	MimeType         string
	SizeBytes        int64
	DurationSeconds  float64
	StoragePath      string
	CoverPath        string
	Chapters         []audiobookChapter
	UploadedByUserID string
	CreatedAt        time.Time
	SeriesID         string
	SeriesName       string
	SeriesSequence   *float64
}

// audiobookSeries is a named grouping of audiobooks (e.g. "Bobiverse"), with
// BookCount filled in by ListSeries for display purposes.
type audiobookSeries struct {
	ID              string
	Name            string
	CreatedByUserID string
	CreatedAt       time.Time
	BookCount       int
	// TopBooks holds up to the first 4 books in the series, in series order,
	// for a card-style thumbnail preview - never the whole series.
	TopBooks []audiobookSeriesTopBook
}

// audiobookSeriesTopBook is one entry in audiobookSeries.TopBooks: just
// enough to build a thumbnail (or an empty placeholder, when CoverPath is
// blank) without fetching the full audiobook record.
type audiobookSeriesTopBook struct {
	ID        string `json:"id"`
	CoverPath string `json:"cover_path"`
}

// audiobookListFilter narrows ListAudiobooks to one of the library's
// subsections. At most one of Favorite/Started/Finished is expected to be
// set; SeriesID can be combined with any of them.
type audiobookListFilter struct {
	Favorite bool
	Started  bool
	Finished bool
	SeriesID string
}

// audiobookChapter is one chapter marker read from the file's own chapter
// atom (common in M4B audiobooks). JSON tags are snake_case since this struct
// is both the JSONB payload stored in Postgres and, via writeJSON, the shape
// sent straight to the frontend.
type audiobookChapter struct {
	Title        string  `json:"title"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
}

type audiobookWithProgress struct {
	audiobookRecord
	PositionSeconds float64
	ProgressAt      time.Time
	IsFavorite      bool
}

// audiobookBookmark is a user-created marker at a point in an audiobook -
// either made by hand ("+ Bookmark") or automatically when a sleep timer is
// started, so a listener who drifted off has a marker for roughly where that
// happened.
type audiobookBookmark struct {
	ID              string
	AudiobookID     string
	UserID          string
	PositionSeconds float64
	Label           string
	CreatedAt       time.Time
}

// audiobookPlaybackSession is a snapshot of another device's most recent
// heartbeat for the same audiobook+user - enough to tell the listener "you're
// also playing this somewhere else right now, at 12:34".
type audiobookPlaybackSession struct {
	PositionSeconds float64
	UpdatedAt       time.Time
}

type audiobookUpload struct {
	ID             string
	UserID         string
	Title          string
	Author         string
	Filename       string
	MimeType       string
	TotalBytes     int64
	ReceivedBytes  int64
	ReceivedRanges []uploadByteRange
	TempPath       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// uploadByteRange is a half-open [Start, End) span of an upload's temp file
// that has been fully written to disk. Chunked uploads record one of these
// per successful PATCH; several can land out of order when a client sends
// multiple chunks over concurrent connections, so progress is tracked as a
// merged set of ranges rather than a single running offset.
type uploadByteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// mergeUploadRanges inserts [start, end) into ranges and collapses any
// overlapping or touching spans, returning a sorted, disjoint result. It is
// idempotent: re-adding a range (a retried chunk, or two concurrent requests
// racing on the same bytes) never double-counts it.
func mergeUploadRanges(ranges []uploadByteRange, start, end int64) []uploadByteRange {
	combined := append(append([]uploadByteRange{}, ranges...), uploadByteRange{Start: start, End: end})
	sort.Slice(combined, func(i, j int) bool { return combined[i].Start < combined[j].Start })

	merged := make([]uploadByteRange, 0, len(combined))
	for _, r := range combined {
		if n := len(merged); n > 0 && r.Start <= merged[n-1].End {
			if r.End > merged[n-1].End {
				merged[n-1].End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func sumUploadRanges(ranges []uploadByteRange) int64 {
	var total int64
	for _, r := range ranges {
		total += r.End - r.Start
	}
	return total
}

// audiobookUploadSeed is the client-supplied metadata for a new upload
// session.
type audiobookUploadSeed struct {
	UserID     string
	Title      string
	Author     string
	Filename   string
	MimeType   string
	TotalBytes int64
}

type audiobookStore interface {
	CreateUpload(ctx context.Context, seed audiobookUploadSeed) (audiobookUpload, error)
	GetUpload(ctx context.Context, id, userID string) (audiobookUpload, error)
	RecordUploadRange(ctx context.Context, id, userID string, start, end int64) (audiobookUpload, error)
	CompleteUpload(ctx context.Context, id, userID string, durationSeconds float64) (audiobookRecord, error)
	AbortUpload(ctx context.Context, id, userID string) (audiobookUpload, error)
	StaleUploads(ctx context.Context, olderThan time.Time) ([]audiobookUpload, error)
	DiscardUpload(ctx context.Context, id string) error

	ListAudiobooks(ctx context.Context, userID string, filter audiobookListFilter) ([]audiobookWithProgress, error)
	GetAudiobook(ctx context.Context, id string) (audiobookRecord, error)
	DeleteAudiobook(ctx context.Context, id string) (audiobookRecord, error)
	SaveProgress(ctx context.Context, audiobookID, userID string, positionSeconds float64) error

	AddFavorite(ctx context.Context, audiobookID, userID string) error
	RemoveFavorite(ctx context.Context, audiobookID, userID string) error

	CreateSeries(ctx context.Context, name, createdByUserID string) (audiobookSeries, error)
	ListSeries(ctx context.Context) ([]audiobookSeries, error)
	RenameSeries(ctx context.Context, id, name string) (audiobookSeries, error)
	DeleteSeries(ctx context.Context, id string) error
	SetAudiobookSeries(ctx context.Context, audiobookID string, seriesID *string, sequence *float64) (audiobookRecord, error)

	// BackfillMetadata re-probes already-stored audiobooks that are still
	// missing cover art, chapters, or a real duration - typically ones
	// uploaded before this extraction existed - using the file already on
	// disk, so nothing needs to be re-uploaded. It returns how many rows
	// picked up something new.
	BackfillMetadata(ctx context.Context) (int, error)

	CreateBookmark(ctx context.Context, audiobookID, userID string, positionSeconds float64, label string) (audiobookBookmark, error)
	ListBookmarks(ctx context.Context, audiobookID, userID string) ([]audiobookBookmark, error)
	DeleteBookmark(ctx context.Context, id, userID string) error

	// HeartbeatPlaybackSession records this device's current playback state for
	// an audiobook and reports whether another device has asked it to stop
	// (clearing that request as it does so).
	HeartbeatPlaybackSession(ctx context.Context, audiobookID, userID, deviceID string, positionSeconds float64, isPlaying bool) (stopRequested bool, err error)
	// OtherActivePlaybackSession returns the most recently updated OTHER
	// device's playback session for this audiobook+user that is still playing
	// and newer than activeSince, or ok=false if there is none.
	OtherActivePlaybackSession(ctx context.Context, audiobookID, userID, deviceID string, activeSince time.Time) (session audiobookPlaybackSession, ok bool, err error)
	// RequestOtherPlaybackSessionsStop asks every other device's session for
	// this audiobook+user to pause on its next heartbeat.
	RequestOtherPlaybackSessionsStop(ctx context.Context, audiobookID, userID, deviceID string) error
}

type postgresAudiobookStore struct {
	pool      *pgxpool.Pool
	dir       string
	tmpDir    string
	coversDir string
}

func newAudiobookStore(ctx context.Context, databaseURL, dataDir string) (audiobookStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, nil
	}

	pool, err := accessor.NewPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect audiobook store: %w", err)
	}

	dir := filepath.Join(dataDir, "audiobooks")
	tmpDir := filepath.Join(dir, "tmp")
	coversDir := filepath.Join(dir, "covers")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create audiobooks directory: %w", err)
	}
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create audiobook covers directory: %w", err)
	}

	store := &postgresAudiobookStore{pool: pool, dir: dir, tmpDir: tmpDir, coversDir: coversDir}
	if err := dbschema.Apply(ctx, pool, "015_audiobooks.sql", "016_audiobook_cover_art.sql", "017_audiobook_chapters.sql", "018_audiobook_narrator.sql", "019_audiobook_bookmarks.sql", "021_audiobook_series.sql", "022_audiobook_favorites.sql", "024_audiobook_upload_ranges.sql", "025_audiobook_playback_sessions.sql"); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure audiobooks schema: %w", err)
	}
	return store, nil
}

func (s *postgresAudiobookStore) CreateUpload(ctx context.Context, seed audiobookUploadSeed) (audiobookUpload, error) {
	id := uuid.Must(uuid.NewV6()).String()
	tempPath := filepath.Join(s.tmpDir, id+".part")

	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("create upload temp file: %w", err)
	}
	// Pre-size the temp file to its final length up front. Chunks now land at
	// arbitrary offsets from several concurrent requests instead of only ever
	// being appended, so every chunk writer needs the file already long enough
	// to seek into - there is no single "end" to append to anymore.
	truncErr := file.Truncate(seed.TotalBytes)
	closeErr := file.Close()
	if truncErr != nil {
		_ = os.Remove(tempPath)
		return audiobookUpload{}, fmt.Errorf("preallocate upload temp file: %w", truncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return audiobookUpload{}, fmt.Errorf("create upload temp file: %w", closeErr)
	}

	var upload audiobookUpload
	err = s.pool.QueryRow(ctx, `
		INSERT INTO audiobook_uploads (id, user_id, title, author, filename, mime_type, total_bytes, temp_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, temp_path, created_at, updated_at
	`, id, seed.UserID, seed.Title, seed.Author, seed.Filename, seed.MimeType, seed.TotalBytes, tempPath).Scan(
		&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
		&upload.TotalBytes, &upload.ReceivedBytes, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
	)
	if err != nil {
		_ = os.Remove(tempPath)
		return audiobookUpload{}, fmt.Errorf("record audiobook upload: %w", err)
	}
	upload.ReceivedRanges = []uploadByteRange{}
	return upload, nil
}

func (s *postgresAudiobookStore) GetUpload(ctx context.Context, id, userID string) (audiobookUpload, error) {
	var upload audiobookUpload
	var rangesJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, received_ranges, temp_path, created_at, updated_at
		FROM audiobook_uploads
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
		&upload.TotalBytes, &upload.ReceivedBytes, &rangesJSON, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookUpload{}, ErrAudiobookUploadNotFound
	}
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("get audiobook upload: %w", err)
	}
	if err := json.Unmarshal(rangesJSON, &upload.ReceivedRanges); err != nil {
		return audiobookUpload{}, fmt.Errorf("decode audiobook upload ranges: %w", err)
	}
	return upload, nil
}

// RecordUploadRange merges [start, end) into the upload's received-ranges
// set and recomputes received_bytes from the merged result. The caller is
// expected to have already written those bytes to the upload's temp file at
// that exact offset before calling this. The read-modify-write happens under
// a row lock (SELECT ... FOR UPDATE) rather than a single compare-and-set,
// because chunks can now arrive out of order from several concurrent
// requests for the same upload - merging is idempotent, so a retried or
// overlapping range is simply absorbed rather than rejected.
func (s *postgresAudiobookStore) RecordUploadRange(ctx context.Context, id, userID string, start, end int64) (audiobookUpload, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("begin audiobook upload range: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rangesJSON []byte
	var upload audiobookUpload
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, received_ranges, temp_path, created_at, updated_at
		FROM audiobook_uploads
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, id, userID).Scan(
		&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
		&upload.TotalBytes, &upload.ReceivedBytes, &rangesJSON, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookUpload{}, ErrAudiobookUploadNotFound
	}
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("lock audiobook upload: %w", err)
	}

	var ranges []uploadByteRange
	if err := json.Unmarshal(rangesJSON, &ranges); err != nil {
		return audiobookUpload{}, fmt.Errorf("decode audiobook upload ranges: %w", err)
	}
	merged := mergeUploadRanges(ranges, start, end)
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("encode audiobook upload ranges: %w", err)
	}
	receivedBytes := sumUploadRanges(merged)

	if _, err := tx.Exec(ctx, `
		UPDATE audiobook_uploads
		SET received_ranges = $3, received_bytes = $4, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, id, userID, mergedJSON, receivedBytes); err != nil {
		return audiobookUpload{}, fmt.Errorf("update audiobook upload range: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return audiobookUpload{}, fmt.Errorf("commit audiobook upload range: %w", err)
	}

	upload.ReceivedRanges = merged
	upload.ReceivedBytes = receivedBytes
	return upload, nil
}

func (s *postgresAudiobookStore) CompleteUpload(ctx context.Context, id, userID string, fallbackDurationSeconds float64) (audiobookRecord, error) {
	upload, err := s.GetUpload(ctx, id, userID)
	if err != nil {
		return audiobookRecord{}, err
	}
	if upload.ReceivedBytes != upload.TotalBytes {
		return audiobookRecord{}, fmt.Errorf("upload incomplete: received %d of %d bytes", upload.ReceivedBytes, upload.TotalBytes)
	}

	ext := strings.ToLower(filepath.Ext(upload.Filename))
	finalID := uuid.Must(uuid.NewV6()).String()
	finalPath := filepath.Join(s.dir, finalID+ext)
	if err := os.Rename(upload.TempPath, finalPath); err != nil {
		return audiobookRecord{}, fmt.Errorf("move completed audiobook: %w", err)
	}

	// The client-supplied mimeType (the browser's File.type, trivially
	// overridable by anyone calling the API directly) is never trusted as the
	// Content-Type the media endpoint later serves - that would let an
	// uploader stage stored XSS against any other viewer by claiming
	// "text/html" for a file full of markup. Sniff it from the file's actual
	// bytes instead, the same way direct_media.go does for linked media.
	mimeType := sniffAudiobookMimeType(finalPath)

	// Prefer whatever the file itself says over a user-typed title/author and
	// the client's best-effort duration: an audiobook's ID3/MP4 tags are
	// usually more complete and more accurate than what someone would bother
	// typing into an upload form.
	title, author, duration := upload.Title, upload.Author, fallbackDurationSeconds
	var narrator string
	var chapters []audiobookChapter
	if probed, probeErr := accessor.ProbeAudioMetadata(finalPath); probeErr == nil {
		if title == "" {
			title = probed.Title
		}
		if author == "" {
			author = probed.Artist
		}
		narrator = probed.Narrator
		if probed.DurationSeconds > 0 {
			duration = probed.DurationSeconds
		}
		for _, c := range probed.Chapters {
			chapters = append(chapters, audiobookChapter{Title: c.Title, StartSeconds: c.StartSeconds, EndSeconds: c.EndSeconds})
		}
	} else {
		log.Printf("audiobooks: metadata probe for %s failed (continuing without it): %v", upload.Filename, probeErr)
	}
	if title == "" {
		title = strings.TrimSuffix(upload.Filename, ext)
	}
	if title == "" {
		title = upload.Filename
	}

	// Reject (and discard) a file that duplicates an audiobook already in the
	// library, rather than silently stacking two copies of the same title.
	if duplicate, err := s.audiobookTitleExists(ctx, title); err != nil {
		_ = os.Remove(finalPath)
		return audiobookRecord{}, fmt.Errorf("check for duplicate audiobook: %w", err)
	} else if duplicate {
		_ = os.Remove(finalPath)
		_ = s.DiscardUpload(ctx, id)
		return audiobookRecord{}, ErrAudiobookDuplicateTitle
	}

	chaptersJSON, err := json.Marshal(chapters)
	if err != nil {
		return audiobookRecord{}, fmt.Errorf("encode audiobook chapters: %w", err)
	}

	coverPath := filepath.Join(s.coversDir, finalID+".jpg")
	if err := accessor.ExtractEmbeddedCoverArt(finalPath, coverPath); err != nil {
		// Most audio files simply have no embedded picture - this is the
		// common case, not a problem worth logging.
		coverPath = ""
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		_ = os.Remove(finalPath)
		if coverPath != "" {
			_ = os.Remove(coverPath)
		}
		return audiobookRecord{}, fmt.Errorf("begin complete upload: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var record audiobookRecord
	var recordChaptersJSON []byte
	err = tx.QueryRow(ctx, `
		INSERT INTO audiobooks (id, title, author, narrator, filename, mime_type, size_bytes, duration_seconds, storage_path, cover_path, chapters, uploaded_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, title, author, narrator, filename, mime_type, size_bytes, duration_seconds, storage_path, cover_path, chapters, uploaded_by_user_id, created_at
	`, finalID, title, author, narrator, upload.Filename, mimeType, upload.TotalBytes, duration, finalPath, coverPath, chaptersJSON, userID).Scan(
		&record.ID, &record.Title, &record.Author, &record.Narrator, &record.Filename, &record.MimeType,
		&record.SizeBytes, &record.DurationSeconds, &record.StoragePath, &record.CoverPath, &recordChaptersJSON, &record.UploadedByUserID, &record.CreatedAt,
	)
	if err == nil {
		if unmarshalErr := json.Unmarshal(recordChaptersJSON, &record.Chapters); unmarshalErr != nil {
			err = fmt.Errorf("decode audiobook chapters: %w", unmarshalErr)
		}
	}
	if err != nil {
		_ = os.Remove(finalPath)
		if coverPath != "" {
			_ = os.Remove(coverPath)
		}
		return audiobookRecord{}, fmt.Errorf("record completed audiobook: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM audiobook_uploads WHERE id = $1`, id); err != nil {
		_ = os.Remove(finalPath)
		if coverPath != "" {
			_ = os.Remove(coverPath)
		}
		return audiobookRecord{}, fmt.Errorf("clear audiobook upload: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		_ = os.Remove(finalPath)
		if coverPath != "" {
			_ = os.Remove(coverPath)
		}
		return audiobookRecord{}, fmt.Errorf("commit completed audiobook: %w", err)
	}
	return record, nil
}

// audiobookTitleExists reports whether the library already has an audiobook
// with the given title, compared case-insensitively.
func (s *postgresAudiobookStore) audiobookTitleExists(ctx context.Context, title string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audiobooks WHERE lower(title) = lower($1))`, title).Scan(&exists); err != nil {
		return false, fmt.Errorf("check duplicate audiobook title: %w", err)
	}
	return exists, nil
}

// sniffAudiobookMimeType reads the stored file's own leading bytes to
// determine its Content-Type, rather than trusting whatever the uploading
// client claimed. Falls back to a generic binary type if the file can't be
// read, which is safe to serve but never renders as anything interactive.
func sniffAudiobookMimeType(path string) string {
	const fallback = "application/octet-stream"
	file, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer func() { _ = file.Close() }()

	prefix := make([]byte, 512)
	n, err := file.Read(prefix)
	if err != nil && !errors.Is(err, io.EOF) {
		return fallback
	}
	return http.DetectContentType(prefix[:n])
}

func (s *postgresAudiobookStore) AbortUpload(ctx context.Context, id, userID string) (audiobookUpload, error) {
	upload, err := s.GetUpload(ctx, id, userID)
	if err != nil {
		return audiobookUpload{}, err
	}
	if err := s.DiscardUpload(ctx, id); err != nil {
		return audiobookUpload{}, err
	}
	_ = os.Remove(upload.TempPath)
	return upload, nil
}

func (s *postgresAudiobookStore) DiscardUpload(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM audiobook_uploads WHERE id = $1`, id); err != nil {
		return fmt.Errorf("discard audiobook upload: %w", err)
	}
	return nil
}

// StaleUploads returns upload sessions that have not received a chunk since
// before the given time, i.e. a tab closed or a device went away mid-upload
// and left a temp file behind. The caller removes the temp file and the row
// via DiscardUpload once it has acted on each one.
func (s *postgresAudiobookStore) StaleUploads(ctx context.Context, olderThan time.Time) ([]audiobookUpload, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, temp_path, created_at, updated_at
		FROM audiobook_uploads
		WHERE updated_at < $1
	`, olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stale audiobook uploads: %w", err)
	}
	defer rows.Close()

	out := []audiobookUpload{}
	for rows.Next() {
		var upload audiobookUpload
		if err := rows.Scan(
			&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
			&upload.TotalBytes, &upload.ReceivedBytes, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan stale audiobook upload: %w", err)
		}
		out = append(out, upload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale audiobook uploads: %w", err)
	}
	return out, nil
}

// audiobookFinishedFraction is the fraction of an audiobook's duration at or
// past which it is considered "finished" rather than merely "started" - the
// same 99% threshold the frontend uses to decide between a "Resume" and a
// "Play" label on the card.
const audiobookFinishedFraction = 0.99

func (s *postgresAudiobookStore) ListAudiobooks(ctx context.Context, userID string, filter audiobookListFilter) ([]audiobookWithProgress, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT
			a.id, a.title, a.author, a.narrator, a.filename, a.mime_type, a.size_bytes, a.duration_seconds,
			a.storage_path, a.cover_path, a.chapters, a.uploaded_by_user_id, a.created_at,
			a.series_id, COALESCE(s.name, ''), a.series_sequence,
			COALESCE(p.position_seconds, 0), p.updated_at,
			(f.audiobook_id IS NOT NULL)
		FROM audiobooks a
		LEFT JOIN audiobook_progress p ON p.audiobook_id = a.id AND p.user_id = $1
		LEFT JOIN audiobook_favorites f ON f.audiobook_id = a.id AND f.user_id = $1
		LEFT JOIN audiobook_series s ON s.id = a.series_id
		WHERE 1 = 1
	`)
	args := []any{userID}
	if filter.Favorite {
		query.WriteString(" AND f.audiobook_id IS NOT NULL")
	}
	if filter.Started {
		_, _ = fmt.Fprintf(&query, " AND p.position_seconds > 0 AND (a.duration_seconds <= 0 OR p.position_seconds < a.duration_seconds * %v)", audiobookFinishedFraction)
	}
	if filter.Finished {
		_, _ = fmt.Fprintf(&query, " AND a.duration_seconds > 0 AND p.position_seconds >= a.duration_seconds * %v", audiobookFinishedFraction)
	}
	if filter.SeriesID != "" {
		args = append(args, filter.SeriesID)
		_, _ = fmt.Fprintf(&query, " AND a.series_id = $%d", len(args))
	}
	query.WriteString(" ORDER BY a.series_id NULLS LAST, a.series_sequence NULLS LAST, a.created_at DESC")

	rows, err := s.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list audiobooks: %w", err)
	}
	defer rows.Close()

	out := []audiobookWithProgress{}
	for rows.Next() {
		var item audiobookWithProgress
		var chaptersJSON []byte
		var progressAt *time.Time
		var seriesID *string
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Author, &item.Narrator, &item.Filename, &item.MimeType, &item.SizeBytes, &item.DurationSeconds,
			&item.StoragePath, &item.CoverPath, &chaptersJSON, &item.UploadedByUserID, &item.CreatedAt,
			&seriesID, &item.SeriesName, &item.SeriesSequence,
			&item.PositionSeconds, &progressAt,
			&item.IsFavorite,
		); err != nil {
			return nil, fmt.Errorf("scan audiobook: %w", err)
		}
		if err := json.Unmarshal(chaptersJSON, &item.Chapters); err != nil {
			return nil, fmt.Errorf("decode audiobook chapters: %w", err)
		}
		if seriesID != nil {
			item.SeriesID = *seriesID
		}
		if progressAt != nil {
			item.ProgressAt = *progressAt
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobooks: %w", err)
	}
	return out, nil
}

func (s *postgresAudiobookStore) GetAudiobook(ctx context.Context, id string) (audiobookRecord, error) {
	var record audiobookRecord
	var chaptersJSON []byte
	var seriesID *string
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.title, a.author, a.narrator, a.filename, a.mime_type, a.size_bytes, a.duration_seconds,
			a.storage_path, a.cover_path, a.chapters, a.uploaded_by_user_id, a.created_at,
			a.series_id, COALESCE(s.name, ''), a.series_sequence
		FROM audiobooks a
		LEFT JOIN audiobook_series s ON s.id = a.series_id
		WHERE a.id = $1
	`, id).Scan(
		&record.ID, &record.Title, &record.Author, &record.Narrator, &record.Filename, &record.MimeType,
		&record.SizeBytes, &record.DurationSeconds, &record.StoragePath, &record.CoverPath, &chaptersJSON, &record.UploadedByUserID, &record.CreatedAt,
		&seriesID, &record.SeriesName, &record.SeriesSequence,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookRecord{}, ErrAudiobookNotFound
	}
	if err != nil {
		return audiobookRecord{}, fmt.Errorf("get audiobook: %w", err)
	}
	if err := json.Unmarshal(chaptersJSON, &record.Chapters); err != nil {
		return audiobookRecord{}, fmt.Errorf("decode audiobook chapters: %w", err)
	}
	if seriesID != nil {
		record.SeriesID = *seriesID
	}
	return record, nil
}

func (s *postgresAudiobookStore) DeleteAudiobook(ctx context.Context, id string) (audiobookRecord, error) {
	record, err := s.GetAudiobook(ctx, id)
	if err != nil {
		return audiobookRecord{}, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM audiobooks WHERE id = $1`, id); err != nil {
		return audiobookRecord{}, fmt.Errorf("delete audiobook: %w", err)
	}
	return record, nil
}

func (s *postgresAudiobookStore) SaveProgress(ctx context.Context, audiobookID, userID string, positionSeconds float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audiobook_progress (audiobook_id, user_id, position_seconds, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (audiobook_id, user_id) DO UPDATE
		SET position_seconds = EXCLUDED.position_seconds,
			updated_at = NOW()
	`, audiobookID, userID, positionSeconds)
	if err != nil {
		return fmt.Errorf("save audiobook progress: %w", err)
	}
	return nil
}

func (s *postgresAudiobookStore) HeartbeatPlaybackSession(ctx context.Context, audiobookID, userID, deviceID string, positionSeconds float64, isPlaying bool) (bool, error) {
	var stopRequested bool
	// The upsert records this device's heartbeat; "cleared" then checks (and,
	// in the same statement, clears) stop_requested as it stood BEFORE this
	// heartbeat - an UPDATE's WHERE clause evaluates against the pre-update
	// row, so this is an atomic read-and-clear in one round trip rather than a
	// separate read then write that could race with another heartbeat.
	err := s.pool.QueryRow(ctx, `
		WITH upsert AS (
			INSERT INTO audiobook_playback_sessions (audiobook_id, user_id, device_id, position_seconds, is_playing, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (audiobook_id, user_id, device_id) DO UPDATE
			SET position_seconds = EXCLUDED.position_seconds,
				is_playing = EXCLUDED.is_playing,
				updated_at = NOW()
			RETURNING audiobook_id
		),
		cleared AS (
			UPDATE audiobook_playback_sessions s
			SET stop_requested = FALSE
			FROM upsert
			WHERE s.audiobook_id = upsert.audiobook_id AND s.user_id = $2 AND s.device_id = $3 AND s.stop_requested = TRUE
			RETURNING TRUE AS was_set
		)
		SELECT COALESCE((SELECT was_set FROM cleared), FALSE)
	`, audiobookID, userID, deviceID, positionSeconds, isPlaying).Scan(&stopRequested)
	if err != nil {
		return false, fmt.Errorf("heartbeat audiobook playback session: %w", err)
	}
	return stopRequested, nil
}

func (s *postgresAudiobookStore) OtherActivePlaybackSession(ctx context.Context, audiobookID, userID, deviceID string, activeSince time.Time) (audiobookPlaybackSession, bool, error) {
	var session audiobookPlaybackSession
	err := s.pool.QueryRow(ctx, `
		SELECT position_seconds, updated_at
		FROM audiobook_playback_sessions
		WHERE audiobook_id = $1 AND user_id = $2 AND device_id != $3 AND is_playing = TRUE AND updated_at > $4
		ORDER BY updated_at DESC
		LIMIT 1
	`, audiobookID, userID, deviceID, activeSince).Scan(&session.PositionSeconds, &session.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookPlaybackSession{}, false, nil
	}
	if err != nil {
		return audiobookPlaybackSession{}, false, fmt.Errorf("get other active audiobook playback session: %w", err)
	}
	return session, true, nil
}

func (s *postgresAudiobookStore) RequestOtherPlaybackSessionsStop(ctx context.Context, audiobookID, userID, deviceID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE audiobook_playback_sessions
		SET stop_requested = TRUE, is_playing = FALSE
		WHERE audiobook_id = $1 AND user_id = $2 AND device_id != $3
	`, audiobookID, userID, deviceID)
	if err != nil {
		return fmt.Errorf("request audiobook playback sessions stop: %w", err)
	}
	return nil
}

func (s *postgresAudiobookStore) BackfillMetadata(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, storage_path, filename, title, author, narrator
		FROM audiobooks
		WHERE cover_path = '' OR chapters = '[]'::jsonb OR duration_seconds <= 0
		   OR title = '' OR title = filename OR author = '' OR narrator = ''
	`)
	if err != nil {
		return 0, fmt.Errorf("list audiobooks needing backfill: %w", err)
	}
	type candidate struct {
		id, storagePath, filename, title, author, narrator string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.storagePath, &c.filename, &c.title, &c.author, &c.narrator); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan audiobook backfill candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	closeErr := rows.Err()
	rows.Close()
	if closeErr != nil {
		return 0, fmt.Errorf("iterate audiobook backfill candidates: %w", closeErr)
	}

	updated := 0
	for _, c := range candidates {
		if s.backfillOne(ctx, c.id, c.storagePath, c.filename, c.title, c.author, c.narrator) {
			updated++
		}
	}
	return updated, nil
}

// backfillOne re-probes a single already-stored audiobook file and applies
// whatever new cover art, chapters, duration, author, or narrator it finds.
// Title is only replaced when it is blank or still exactly the original
// filename - the signature an upload left behind before this extraction
// existed, when the client always filled the title field in with the
// filename itself. Author/narrator are safe to fill in whenever blank: nothing
// ever auto-filled those from a filename, so blank genuinely means unknown. A
// title that differs from the filename is left alone, since at that point it
// cannot be distinguished from one someone typed by hand.
func (s *postgresAudiobookStore) backfillOne(ctx context.Context, id, storagePath, filename, currentTitle, currentAuthor, currentNarrator string) bool {
	probed, probeErr := accessor.ProbeAudioMetadata(storagePath)

	var chaptersJSON []byte
	if probeErr == nil && len(probed.Chapters) > 0 {
		chapters := make([]audiobookChapter, 0, len(probed.Chapters))
		for _, c := range probed.Chapters {
			chapters = append(chapters, audiobookChapter{Title: c.Title, StartSeconds: c.StartSeconds, EndSeconds: c.EndSeconds})
		}
		if encoded, err := json.Marshal(chapters); err == nil {
			chaptersJSON = encoded
		}
	}

	coverPath := filepath.Join(s.coversDir, id+".jpg")
	hasCover := accessor.ExtractEmbeddedCoverArt(storagePath, coverPath) == nil
	if !hasCover {
		coverPath = ""
	}

	var duration float64
	var title, author, narrator string
	if probeErr == nil {
		duration = probed.DurationSeconds
		if currentTitle == "" || currentTitle == filename {
			title = probed.Title
		}
		if currentAuthor == "" {
			author = probed.Artist
		}
		if currentNarrator == "" {
			narrator = probed.Narrator
		}
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE audiobooks
		SET
			cover_path = CASE WHEN cover_path = '' THEN COALESCE(NULLIF($2, ''), cover_path) ELSE cover_path END,
			chapters = CASE WHEN chapters = '[]'::jsonb AND $3::jsonb IS NOT NULL THEN $3::jsonb ELSE chapters END,
			duration_seconds = CASE WHEN duration_seconds <= 0 AND $4 > 0 THEN $4 ELSE duration_seconds END,
			title = CASE WHEN (title = '' OR title = filename) AND $5 <> '' THEN $5 ELSE title END,
			author = CASE WHEN author = '' AND $6 <> '' THEN $6 ELSE author END,
			narrator = CASE WHEN narrator = '' AND $7 <> '' THEN $7 ELSE narrator END
		WHERE id = $1
	`, id, coverPath, chaptersJSON, duration, title, author, narrator)
	if err != nil {
		log.Printf("audiobooks: backfill metadata for %s failed: %v", id, err)
		if hasCover {
			_ = os.Remove(coverPath)
		}
		return false
	}
	return hasCover || len(chaptersJSON) > 0 || duration > 0 || title != "" || author != "" || narrator != ""
}

func (s *postgresAudiobookStore) CreateBookmark(ctx context.Context, audiobookID, userID string, positionSeconds float64, label string) (audiobookBookmark, error) {
	var bookmark audiobookBookmark
	err := s.pool.QueryRow(ctx, `
		INSERT INTO audiobook_bookmarks (id, audiobook_id, user_id, position_seconds, label)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, audiobook_id, user_id, position_seconds, label, created_at
	`, uuid.Must(uuid.NewV6()).String(), audiobookID, userID, positionSeconds, label).Scan(
		&bookmark.ID, &bookmark.AudiobookID, &bookmark.UserID, &bookmark.PositionSeconds, &bookmark.Label, &bookmark.CreatedAt,
	)
	if err != nil {
		return audiobookBookmark{}, fmt.Errorf("create audiobook bookmark: %w", err)
	}
	return bookmark, nil
}

func (s *postgresAudiobookStore) ListBookmarks(ctx context.Context, audiobookID, userID string) ([]audiobookBookmark, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, audiobook_id, user_id, position_seconds, label, created_at
		FROM audiobook_bookmarks
		WHERE audiobook_id = $1 AND user_id = $2
		ORDER BY created_at DESC
	`, audiobookID, userID)
	if err != nil {
		return nil, fmt.Errorf("list audiobook bookmarks: %w", err)
	}
	defer rows.Close()

	out := []audiobookBookmark{}
	for rows.Next() {
		var bookmark audiobookBookmark
		if err := rows.Scan(&bookmark.ID, &bookmark.AudiobookID, &bookmark.UserID, &bookmark.PositionSeconds, &bookmark.Label, &bookmark.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audiobook bookmark: %w", err)
		}
		out = append(out, bookmark)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobook bookmarks: %w", err)
	}
	return out, nil
}

func (s *postgresAudiobookStore) DeleteBookmark(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audiobook_bookmarks WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete audiobook bookmark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAudiobookBookmarkNotFound
	}
	return nil
}

func (s *postgresAudiobookStore) AddFavorite(ctx context.Context, audiobookID, userID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audiobook_favorites (audiobook_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (audiobook_id, user_id) DO NOTHING
	`, audiobookID, userID)
	if err != nil {
		return fmt.Errorf("add audiobook favorite: %w", err)
	}
	return nil
}

func (s *postgresAudiobookStore) RemoveFavorite(ctx context.Context, audiobookID, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM audiobook_favorites WHERE audiobook_id = $1 AND user_id = $2`, audiobookID, userID)
	if err != nil {
		return fmt.Errorf("remove audiobook favorite: %w", err)
	}
	return nil
}

func (s *postgresAudiobookStore) CreateSeries(ctx context.Context, name, createdByUserID string) (audiobookSeries, error) {
	var series audiobookSeries
	err := s.pool.QueryRow(ctx, `
		INSERT INTO audiobook_series (id, name, created_by_user_id)
		VALUES ($1, $2, $3)
		RETURNING id, name, created_by_user_id, created_at
	`, uuid.Must(uuid.NewV6()).String(), name, createdByUserID).Scan(
		&series.ID, &series.Name, &series.CreatedByUserID, &series.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return audiobookSeries{}, ErrAudiobookSeriesNameTaken
		}
		return audiobookSeries{}, fmt.Errorf("create audiobook series: %w", err)
	}
	return series, nil
}

func (s *postgresAudiobookStore) ListSeries(ctx context.Context) ([]audiobookSeries, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.name, s.created_by_user_id, s.created_at, COUNT(a.id), COALESCE(top.books, '[]'::jsonb)
		FROM audiobook_series s
		LEFT JOIN audiobooks a ON a.series_id = s.id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object('id', t.id, 'cover_path', t.cover_path) ORDER BY t.series_sequence NULLS LAST, t.created_at) AS books
			FROM (
				SELECT id, cover_path, series_sequence, created_at
				FROM audiobooks
				WHERE series_id = s.id
				ORDER BY series_sequence NULLS LAST, created_at
				LIMIT 4
			) t
		) top ON true
		GROUP BY s.id, top.books
		ORDER BY lower(s.name) ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list audiobook series: %w", err)
	}
	defer rows.Close()

	out := []audiobookSeries{}
	for rows.Next() {
		var series audiobookSeries
		var topBooksJSON []byte
		if err := rows.Scan(&series.ID, &series.Name, &series.CreatedByUserID, &series.CreatedAt, &series.BookCount, &topBooksJSON); err != nil {
			return nil, fmt.Errorf("scan audiobook series: %w", err)
		}
		if err := json.Unmarshal(topBooksJSON, &series.TopBooks); err != nil {
			return nil, fmt.Errorf("decode audiobook series top books: %w", err)
		}
		out = append(out, series)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobook series: %w", err)
	}
	return out, nil
}

func (s *postgresAudiobookStore) RenameSeries(ctx context.Context, id, name string) (audiobookSeries, error) {
	var series audiobookSeries
	err := s.pool.QueryRow(ctx, `
		UPDATE audiobook_series SET name = $2 WHERE id = $1
		RETURNING id, name, created_by_user_id, created_at
	`, id, name).Scan(&series.ID, &series.Name, &series.CreatedByUserID, &series.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookSeries{}, ErrAudiobookSeriesNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return audiobookSeries{}, ErrAudiobookSeriesNameTaken
		}
		return audiobookSeries{}, fmt.Errorf("rename audiobook series: %w", err)
	}
	return series, nil
}

func (s *postgresAudiobookStore) DeleteSeries(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audiobook_series WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete audiobook series: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAudiobookSeriesNotFound
	}
	return nil
}

// SetAudiobookSeries assigns (or clears, when seriesID is nil) an audiobook's
// series membership and its position within it.
func (s *postgresAudiobookStore) SetAudiobookSeries(ctx context.Context, audiobookID string, seriesID *string, sequence *float64) (audiobookRecord, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE audiobooks SET series_id = $2, series_sequence = $3 WHERE id = $1
	`, audiobookID, seriesID, sequence)
	if err != nil {
		return audiobookRecord{}, fmt.Errorf("set audiobook series: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return audiobookRecord{}, ErrAudiobookNotFound
	}
	return s.GetAudiobook(ctx, audiobookID)
}
