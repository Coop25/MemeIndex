package accessor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/dbschema"
)

type PostgresStore struct {
	pool       *pgxpool.Pool
	uploadDir  string
	previewDir string
	dataDir    string

	// searchTrgmReady tracks whether the optional pg_trgm search indexes were
	// created. When false, filename/notes search falls back to slower scans and
	// callers surface a "degraded" signal on /readyz and the admin dashboard.
	searchTrgmReady atomic.Bool
}

// SearchIndexAvailable reports whether the optional trigram search indexes are
// in place. It is false only when the database role could not enable pg_trgm.
func (s *PostgresStore) SearchIndexAvailable() bool {
	return s.searchTrgmReady.Load()
}

func NewPostgresStore(ctx context.Context, databaseURL string, dataDir string) (*PostgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database url is required")
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	uploadDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	store := &PostgresStore{
		pool:       pool,
		uploadDir:  uploadDir,
		previewDir: filepath.Join(dataDir, "thumbnails"),
		dataDir:    dataDir,
	}

	if err := store.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.importLegacyDataIfNeeded(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.importLegacyReelSessionsIfNeeded(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.backfillMissingContentHashes(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *PostgresStore) UploadDir() string {
	return s.uploadDir
}

// Ping verifies the connection pool can reach PostgreSQL. It backs the /readyz
// endpoint so orchestration can tell a database outage apart from a healthy
// process.
func (s *PostgresStore) Ping(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("postgres pool is not initialised")
	}
	return s.pool.Ping(ctx)
}

// Close releases the connection pool. It is meant to be called once during a
// graceful shutdown, after the HTTP server has stopped accepting requests.
func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) ThumbnailDir() string {
	return s.previewDir
}

func (s *PostgresStore) EnsurePreviewAssets() error {
	// Only video and image rows get thumbnails, and only their stored name and
	// content type are needed - not the per-row tag aggregation and favourites
	// lookup that List() hydrates for the whole archive.
	memes, err := s.videoMemesForPreview(context.Background())
	if err != nil {
		return fmt.Errorf("preview asset backfill: list videos: %w", err)
	}
	totalVideos := len(memes)
	generated := 0
	existing := 0
	failed := 0

	if totalVideos == 0 {
		log.Printf("preview asset backfill: no video memes found")
		return nil
	}

	log.Printf("preview asset backfill: starting postgres video thumbnails for %d video(s)", totalVideos)

	processedVideos := 0
	for i := range memes {
		result, err := ensurePreviewAssetWithResult(s.uploadDir, s.previewDir, &memes[i])
		processedVideos += 1
		switch result {
		case previewAssetGenerated:
			generated += 1
		case previewAssetAlreadyExists:
			existing += 1
		}
		if err != nil {
			failed += 1
		}

		if processedVideos%25 == 0 || processedVideos == totalVideos {
			log.Printf(
				"preview asset backfill: processed %d/%d videos (generated=%d existing=%d failed=%d)",
				processedVideos,
				totalVideos,
				generated,
				existing,
				failed,
			)
		}

		if err != nil {
			continue
		}
	}

	log.Printf(
		"preview asset backfill: finished postgres video thumbnails (total=%d generated=%d existing=%d failed=%d)",
		totalVideos,
		generated,
		existing,
		failed,
	)
	return nil
}

// videoMemesForPreview returns the minimal shape the thumbnail backfill needs:
// every visible video meme's id, stored name, and content type, newest first.
func (s *PostgresStore) videoMemesForPreview(ctx context.Context) ([]Meme, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, stored_name, content_type
		FROM memes
		WHERE (content_type LIKE 'video/%' OR content_type LIKE 'image/%')
		  AND COALESCE(hidden_from_app, FALSE) = FALSE
		ORDER BY created_at DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memes []Meme
	for rows.Next() {
		var meme Meme
		if err := rows.Scan(&meme.ID, &meme.StoredName, &meme.ContentType); err != nil {
			return nil, err
		}
		memes = append(memes, meme)
	}
	return memes, rows.Err()
}

// tableExists reports whether a relation is present in the database. Used for
// tables owned by other packages' schemas (audiobooks), which may not exist
// when this package runs standalone.
func tableExists(ctx context.Context, db queryable, name string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check %s exists: %w", name, err)
	}
	return exists, nil
}

func scanIDList(ctx context.Context, q queryable, sql string, args ...any) ([]string, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	if err := dbschema.Apply(ctx, s.pool,
		"001_memes_core.sql",
		"002_memes_compat.sql",
		"006_memes_source_url.sql",
		"007_memes_suggested_tags.sql",
		"008_memes_auto_suggest_disabled.sql",
		"009_meme_shares.sql",
		"010_meme_query_indexes.sql",
		"012_meme_search_text.sql",
		"030_meme_share_links.sql",
	); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	// Trigram search indexes are a speed-up, not a correctness requirement, and
	// need the pg_trgm extension a restricted role may not be able to create.
	notApplied := dbschema.ApplyOptional(ctx, s.pool, "011_meme_search_trgm.sql")
	s.searchTrgmReady.Store(len(notApplied) == 0)
	if len(notApplied) > 0 {
		log.Printf("dbschema: search running in degraded mode (pg_trgm indexes unavailable): %v", notApplied)
	}
	return nil
}

type queryable interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func scanInt64List(ctx context.Context, q queryable, sql string, args ...any) ([]int64, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AllStoredFiles returns every meme row, hidden and pending-delete included.
func (s *PostgresStore) AllStoredFiles() ([]StoredFile, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, original_name, stored_name, file_path, COALESCE(hidden_from_app, FALSE), size_bytes
		FROM memes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StoredFile, 0)
	for rows.Next() {
		var f StoredFile
		if err := rows.Scan(&f.ID, &f.Title, &f.StoredName, &f.FilePath, &f.Hidden, &f.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *PostgresStore) countByUser(query string, args ...any) (map[string]int, error) {
	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}
