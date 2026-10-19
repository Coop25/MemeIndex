package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/accessor"
	"memeindex/internal/dbschema"
)

// ErrAudiobookUploadNotFound is returned when an upload session id does not
// exist, has already been finalized/aborted, or belongs to a different user.
var ErrAudiobookUploadNotFound = errors.New("audiobook upload not found")

// ErrAudiobookUploadOffsetConflict is returned when a chunk's declared
// Upload-Offset does not match how many bytes the server has already
// received. The client is expected to re-read the upload's status and retry
// from the server's offset, which is what makes the upload resumable.
var ErrAudiobookUploadOffsetConflict = errors.New("audiobook upload offset conflict")

// ErrAudiobookNotFound is returned when a finished audiobook id does not exist.
var ErrAudiobookNotFound = errors.New("audiobook not found")

type audiobookRecord struct {
	ID               string
	Title            string
	Author           string
	Filename         string
	MimeType         string
	SizeBytes        int64
	DurationSeconds  float64
	StoragePath      string
	UploadedByUserID string
	CreatedAt        time.Time
}

type audiobookWithProgress struct {
	audiobookRecord
	PositionSeconds float64
	ProgressAt      time.Time
}

type audiobookUpload struct {
	ID            string
	UserID        string
	Title         string
	Author        string
	Filename      string
	MimeType      string
	TotalBytes    int64
	ReceivedBytes int64
	TempPath      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
	AppendUploadChunk(ctx context.Context, id, userID string, expectedOffset int64, chunkLen int64) (audiobookUpload, error)
	CompleteUpload(ctx context.Context, id, userID string, durationSeconds float64) (audiobookRecord, error)
	AbortUpload(ctx context.Context, id, userID string) (audiobookUpload, error)
	StaleUploads(ctx context.Context, olderThan time.Time) ([]audiobookUpload, error)
	DiscardUpload(ctx context.Context, id string) error

	ListAudiobooks(ctx context.Context, userID string) ([]audiobookWithProgress, error)
	GetAudiobook(ctx context.Context, id string) (audiobookRecord, error)
	DeleteAudiobook(ctx context.Context, id string) (audiobookRecord, error)
	SaveProgress(ctx context.Context, audiobookID, userID string, positionSeconds float64) error
}

type postgresAudiobookStore struct {
	pool   *pgxpool.Pool
	dir    string
	tmpDir string
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
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create audiobooks directory: %w", err)
	}

	store := &postgresAudiobookStore{pool: pool, dir: dir, tmpDir: tmpDir}
	if err := dbschema.Apply(ctx, pool, "015_audiobooks.sql"); err != nil {
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
	file.Close()

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
		os.Remove(tempPath)
		return audiobookUpload{}, fmt.Errorf("record audiobook upload: %w", err)
	}
	return upload, nil
}

func (s *postgresAudiobookStore) GetUpload(ctx context.Context, id, userID string) (audiobookUpload, error) {
	var upload audiobookUpload
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, temp_path, created_at, updated_at
		FROM audiobook_uploads
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
		&upload.TotalBytes, &upload.ReceivedBytes, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookUpload{}, ErrAudiobookUploadNotFound
	}
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("get audiobook upload: %w", err)
	}
	return upload, nil
}

// AppendUploadChunk advances the upload's received-bytes counter by chunkLen,
// but only when expectedOffset matches what the server has already received.
// The caller is expected to have already written chunkLen bytes to the
// upload's temp file at exactly that offset before calling this; the
// atomic compare-and-set here is what keeps the database's notion of
// progress consistent with the file on disk even if two requests for the
// same upload somehow overlap.
func (s *postgresAudiobookStore) AppendUploadChunk(ctx context.Context, id, userID string, expectedOffset int64, chunkLen int64) (audiobookUpload, error) {
	var upload audiobookUpload
	err := s.pool.QueryRow(ctx, `
		UPDATE audiobook_uploads
		SET received_bytes = received_bytes + $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND received_bytes = $4
		RETURNING id, user_id, title, author, filename, mime_type, total_bytes, received_bytes, temp_path, created_at, updated_at
	`, id, userID, chunkLen, expectedOffset).Scan(
		&upload.ID, &upload.UserID, &upload.Title, &upload.Author, &upload.Filename, &upload.MimeType,
		&upload.TotalBytes, &upload.ReceivedBytes, &upload.TempPath, &upload.CreatedAt, &upload.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the upload does not exist/belong to this user, or the offset
		// the caller wrote at no longer matches. Distinguish the two so the
		// handler can tell the client what to do next.
		if _, getErr := s.GetUpload(ctx, id, userID); getErr != nil {
			return audiobookUpload{}, ErrAudiobookUploadNotFound
		}
		return audiobookUpload{}, ErrAudiobookUploadOffsetConflict
	}
	if err != nil {
		return audiobookUpload{}, fmt.Errorf("advance audiobook upload: %w", err)
	}
	return upload, nil
}

func (s *postgresAudiobookStore) CompleteUpload(ctx context.Context, id, userID string, durationSeconds float64) (audiobookRecord, error) {
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		os.Remove(finalPath)
		return audiobookRecord{}, fmt.Errorf("begin complete upload: %w", err)
	}
	defer tx.Rollback(ctx)

	var record audiobookRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO audiobooks (id, title, author, filename, mime_type, size_bytes, duration_seconds, storage_path, uploaded_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, title, author, filename, mime_type, size_bytes, duration_seconds, storage_path, uploaded_by_user_id, created_at
	`, finalID, upload.Title, upload.Author, upload.Filename, upload.MimeType, upload.TotalBytes, durationSeconds, finalPath, userID).Scan(
		&record.ID, &record.Title, &record.Author, &record.Filename, &record.MimeType,
		&record.SizeBytes, &record.DurationSeconds, &record.StoragePath, &record.UploadedByUserID, &record.CreatedAt,
	)
	if err != nil {
		os.Remove(finalPath)
		return audiobookRecord{}, fmt.Errorf("record completed audiobook: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM audiobook_uploads WHERE id = $1`, id); err != nil {
		os.Remove(finalPath)
		return audiobookRecord{}, fmt.Errorf("clear audiobook upload: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		os.Remove(finalPath)
		return audiobookRecord{}, fmt.Errorf("commit completed audiobook: %w", err)
	}
	return record, nil
}

func (s *postgresAudiobookStore) AbortUpload(ctx context.Context, id, userID string) (audiobookUpload, error) {
	upload, err := s.GetUpload(ctx, id, userID)
	if err != nil {
		return audiobookUpload{}, err
	}
	if err := s.DiscardUpload(ctx, id); err != nil {
		return audiobookUpload{}, err
	}
	os.Remove(upload.TempPath)
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

func (s *postgresAudiobookStore) ListAudiobooks(ctx context.Context, userID string) ([]audiobookWithProgress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			a.id, a.title, a.author, a.filename, a.mime_type, a.size_bytes, a.duration_seconds,
			a.storage_path, a.uploaded_by_user_id, a.created_at,
			COALESCE(p.position_seconds, 0), p.updated_at
		FROM audiobooks a
		LEFT JOIN audiobook_progress p ON p.audiobook_id = a.id AND p.user_id = $1
		ORDER BY a.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list audiobooks: %w", err)
	}
	defer rows.Close()

	out := []audiobookWithProgress{}
	for rows.Next() {
		var item audiobookWithProgress
		var progressAt *time.Time
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Author, &item.Filename, &item.MimeType, &item.SizeBytes, &item.DurationSeconds,
			&item.StoragePath, &item.UploadedByUserID, &item.CreatedAt,
			&item.PositionSeconds, &progressAt,
		); err != nil {
			return nil, fmt.Errorf("scan audiobook: %w", err)
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
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, author, filename, mime_type, size_bytes, duration_seconds, storage_path, uploaded_by_user_id, created_at
		FROM audiobooks
		WHERE id = $1
	`, id).Scan(
		&record.ID, &record.Title, &record.Author, &record.Filename, &record.MimeType,
		&record.SizeBytes, &record.DurationSeconds, &record.StoragePath, &record.UploadedByUserID, &record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return audiobookRecord{}, ErrAudiobookNotFound
	}
	if err != nil {
		return audiobookRecord{}, fmt.Errorf("get audiobook: %w", err)
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
