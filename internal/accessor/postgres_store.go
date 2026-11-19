package accessor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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

func (s *PostgresStore) GetByID(userID, id string) (Meme, error) {
	ctx := context.Background()
	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(userID), strings.TrimSpace(id))
}

func (s *PostgresStore) GetAnyByID(id string) (Meme, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $1
		GROUP BY m.id
	`, strings.TrimSpace(id))

	meme, err := scanMemeRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
}

// MemesByIDs resolves a batch of visible memes in one round trip, keyed by id.
// Ids that do not match a visible meme are simply absent from the result, the
// same outcome a per-id GetByID("", id) lookup would produce.
func (s *PostgresStore) MemesByIDs(ids []string) (map[string]Meme, error) {
	out := make(map[string]Meme, len(ids))
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(context.Background(), "SELECT"+memeRowSelectColumns+`
		FROM memes m
		WHERE m.id = ANY($2::text[])
		  AND COALESCE(m.hidden_from_app, FALSE) = FALSE
	`, normalizeFavoriteUserID(""), clean)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		meme, scanErr := scanMemeRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		decoratePreviewPath(&meme, s.previewDir)
		out[meme.ID] = meme
	}
	return out, rows.Err()
}

func (s *PostgresStore) Random(excludedIDs []string) (Meme, error) {
	ctx := context.Background()

	normalizedExcluded := make([]string, 0, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		normalizedExcluded = append(normalizedExcluded, trimmed)
	}

	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT m.id
		FROM memes m
		WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
			AND NOT (m.id = ANY($1::text[]))
		ORDER BY random()
		LIMIT 1
	`, normalizedExcluded).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			SELECT m.id
			FROM memes m
			WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
			ORDER BY random()
			LIMIT 1
		`).Scan(&id)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}

	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(""), id)
}

func (s *PostgresStore) Create(input CreateInput) (Meme, error) {
	id := uuid.Must(uuid.NewV6()).String()
	contentType := detectContentType(input.Header, input.ContentType, input.Filename)
	storedName := id + storedFileExtension(input.Filename, contentType)
	targetPath := filepath.Join(s.uploadDir, storedName)

	dst, err := os.Create(targetPath)
	if err != nil {
		return Meme{}, fmt.Errorf("create upload file: %w", err)
	}

	hasher := newContentHashWriter()
	size, err := io.Copy(io.MultiWriter(dst, hasher), input.File)
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, fmt.Errorf("write upload file: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(targetPath)
		return Meme{}, fmt.Errorf("close upload file: %w", closeErr)
	}

	now := time.Now().UTC()
	meme := Meme{
		ID:                  id,
		OriginalName:        input.Filename,
		StoredName:          storedName,
		FilePath:            "/uploads/" + storedName,
		ContentType:         contentType,
		ContentHash:         contentHashString(hasher),
		SizeBytes:           size,
		Tags:                normalizeTags(input.Tags),
		SuggestedTags:       []string{},
		AutoSuggestDisabled: false,
		Notes:               strings.TrimSpace(input.Notes),
		SourceURL:           strings.TrimSpace(input.SourceURL),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	ctx := context.Background()

	// Check for a duplicate before generating a thumbnail: an ffmpeg run for a
	// file that is about to be rejected is wasted work, and its thumbnail used
	// to be left behind on disk.
	if existing, err := s.getByHash(ctx, meme.ContentHash); err == nil {
		_ = os.Remove(targetPath)
		return Meme{}, s.duplicateError(ctx, existing)
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if err := ensurePreviewAsset(s.uploadDir, s.previewDir, &meme); err != nil {
		// Thumbnail generation is best-effort.
	}
	cleanup := func() { removeUploadAndThumbnail(s.uploadDir, s.previewDir, storedName) }

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		cleanup()
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO memes (
			id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, suggested_tags, auto_suggest_disabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, meme.Notes, meme.SourceURL, meme.SuggestedTags, meme.AutoSuggestDisabled, meme.CreatedAt, meme.UpdatedAt); err != nil {
		cleanup()
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			if existing, lookupErr := s.getByHash(ctx, meme.ContentHash); lookupErr == nil {
				return Meme{}, s.duplicateError(ctx, existing)
			}
		}
		return Meme{}, err
	}

	if err := s.replaceTags(ctx, tx, meme.ID, meme.Tags); err != nil {
		cleanup()
		return Meme{}, err
	}

	if err := s.insertAuditLog(ctx, tx, meme.ID, "uploaded", input.Actor, fmt.Sprintf("Uploaded %s", meme.OriginalName)); err != nil {
		cleanup()
		return Meme{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return Meme{}, err
	}

	return meme, nil
}

// duplicateError builds the error for an upload whose content already exists.
// When that existing copy is hidden pending delete approval it reports so,
// instead of pointing the uploader at a meme they cannot open.
func (s *PostgresStore) duplicateError(ctx context.Context, existing Meme) error {
	var hidden bool
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(hidden_from_app, FALSE) FROM memes WHERE id = $1`, existing.ID).Scan(&hidden); err == nil && hidden {
		return &DuplicateMemeError{PendingDelete: true}
	}
	return &DuplicateMemeError{Existing: existing}
}

// EditMeme applies a partial edit to one meme in a single transaction. It
// locks the meme's row first, so concurrent edits to the same meme run one
// after another and each one's change is worked out against the tags the
// previous one left: two people adding different tags at once both keep
// theirs. Only the tags in the change are linked or unlinked, and notes and
// the viewer's favorite are written only when the edit sets them.
func (s *PostgresStore) EditMeme(userID, id string, edit MemeEdit) (Meme, error) {
	ctx := context.Background()
	userID = normalizeFavoriteUserID(userID)
	id = strings.TrimSpace(id)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	var lockedID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM memes
		WHERE id = $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
		FOR UPDATE
	`, id).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Meme{}, os.ErrNotExist
	}
	if err != nil {
		return Meme{}, err
	}

	// Read after taking the lock: under READ COMMITTED each statement sees
	// everything committed before it started, including an edit this one has
	// just waited on.
	existing, err := s.getByID(ctx, tx, userID, id)
	if err != nil {
		return Meme{}, err
	}
	change, err := planMemeEdit(existing, edit)
	if err != nil {
		return Meme{}, err
	}
	if edit.Authorize != nil {
		if err := edit.Authorize(change); err != nil {
			return Meme{}, err
		}
	}
	if change.Empty() {
		return existing, nil
	}

	if change.Notes != nil || len(change.TagsAdded) > 0 || len(change.TagsRemoved) > 0 {
		// A nil change.Notes binds as NULL and keeps the current notes.
		if _, err := tx.Exec(ctx, `
			UPDATE memes
			SET notes = COALESCE($2::text, notes), updated_at = $3
			WHERE id = $1
		`, id, change.Notes, time.Now().UTC()); err != nil {
			return Meme{}, err
		}
	}
	if err := s.addMemeTags(ctx, tx, id, change.TagsAdded); err != nil {
		return Meme{}, err
	}
	if err := s.removeMemeTags(ctx, tx, id, change.TagsRemoved); err != nil {
		return Meme{}, err
	}
	for _, tag := range change.TagsAdded {
		if err := s.insertAuditLog(ctx, tx, id, "tag_added", edit.Actor, fmt.Sprintf("Added tag %q", tag)); err != nil {
			return Meme{}, err
		}
	}
	for _, tag := range change.TagsRemoved {
		if err := s.insertAuditLog(ctx, tx, id, "tag_removed", edit.Actor, fmt.Sprintf("Removed tag %q", tag)); err != nil {
			return Meme{}, err
		}
	}
	if change.Favorite != nil {
		if err := s.setFavoriteInExecutor(ctx, tx, userID, id, *change.Favorite); err != nil {
			return Meme{}, err
		}
		action, description := "favorited", "Added meme to favorites"
		if !*change.Favorite {
			action, description = "unfavorited", "Removed meme from favorites"
		}
		if err := s.insertAuditLog(ctx, tx, id, action, edit.Actor, description); err != nil {
			return Meme{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Meme{}, err
	}
	return s.GetByID(userID, id)
}

func (s *PostgresStore) SetFavorite(userID, id string, favorite bool) (Meme, error) {
	ctx := context.Background()
	if err := s.setFavoriteInExecutor(ctx, s.pool, userID, strings.TrimSpace(id), favorite); err != nil {
		return Meme{}, err
	}
	return s.GetByID(userID, id)
}

func (s *PostgresStore) SetFavoriteWithActor(userID, id string, favorite bool, actor AuditActor) (Meme, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	if err := s.setFavoriteInExecutor(ctx, tx, userID, strings.TrimSpace(id), favorite); err != nil {
		return Meme{}, err
	}
	action := "favorited"
	description := "Added meme to favorites"
	if !favorite {
		action = "unfavorited"
		description = "Removed meme from favorites"
	}
	if err := s.insertAuditLog(ctx, tx, strings.TrimSpace(id), action, actor, description); err != nil {
		return Meme{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Meme{}, err
	}
	return s.GetByID(userID, id)
}

// HiddenMemeIDs lists the memes hidden from the app pending delete approval.
func (s *PostgresStore) HiddenMemeIDs() ([]string, error) {
	return scanIDList(context.Background(), s.pool, `SELECT id FROM memes WHERE COALESCE(hidden_from_app, FALSE) = TRUE`)
}

func (s *PostgresStore) Delete(input DeleteInput) (DeleteResult, error) {
	ctx := context.Background()
	id := strings.TrimSpace(input.ID)
	var storedName string
	err := s.pool.QueryRow(ctx, `SELECT stored_name FROM memes WHERE id = $1`, id).Scan(&storedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteResult{}, os.ErrNotExist
		}
		return DeleteResult{}, err
	}

	if !input.Actor.IsSuperAdmin {
		_, err := s.pool.Exec(ctx, `
			UPDATE memes
			SET hidden_from_app = TRUE,
				pending_delete = TRUE,
				delete_requested_by_user_id = $2,
				delete_requested_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, id, strings.TrimSpace(input.Actor.UserID))
		if err != nil {
			return DeleteResult{}, err
		}
		if err := s.insertAuditLog(ctx, s.pool, id, "delete_requested", input.Actor, "Requested delete approval"); err != nil {
			return DeleteResult{}, err
		}
		return DeleteResult{PendingApproval: true}, nil
	}

	// Capture the meme's tags before the cascade drops its meme_tags rows so any
	// that are left unreferenced can be pruned (replaceTags no longer sweeps the
	// whole tags table, so each removal path cleans up after itself).
	orphanCandidates, err := scanInt64List(ctx, s.pool, `SELECT tag_id FROM meme_tags WHERE meme_id = $1`, id)
	if err != nil {
		return DeleteResult{}, err
	}

	commandTag, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id)
	if err != nil {
		return DeleteResult{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return DeleteResult{}, os.ErrNotExist
	}

	if err := s.deleteOrphanTags(ctx, s.pool, orphanCandidates); err != nil {
		return DeleteResult{}, err
	}

	if err := s.insertAuditLog(ctx, s.pool, id, "deleted", input.Actor, "Deleted meme"); err != nil {
		return DeleteResult{}, err
	}

	targetPath := filepath.Join(s.uploadDir, storedName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return DeleteResult{}, fmt.Errorf("remove upload file: %w", err)
	}
	thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(storedName))
	if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return DeleteResult{}, fmt.Errorf("remove thumbnail file: %w", err)
	}
	return DeleteResult{Deleted: true}, nil
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

func (s *PostgresStore) getByID(ctx context.Context, db queryable, userID, id string) (Meme, error) {
	rows, err := db.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			EXISTS (
				SELECT 1
				FROM user_favorites uf
				WHERE uf.user_id = $1 AND uf.meme_id = m.id
			) AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $2
			AND COALESCE(m.hidden_from_app, FALSE) = FALSE
		GROUP BY m.id
	`, userID, id)
	if err != nil {
		return Meme{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		return Meme{}, os.ErrNotExist
	}

	meme, err := scanMemeRow(rows)
	if err != nil {
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
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

func (s *PostgresStore) setFavoriteInExecutor(ctx context.Context, db queryable, userID, memeID string, favorite bool) error {
	userID = normalizeFavoriteUserID(userID)

	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memes WHERE id = $1)`, memeID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}

	if favorite {
		_, err := db.Exec(ctx, `
			INSERT INTO user_favorites (user_id, meme_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, userID, memeID)
		return err
	}

	_, err := db.Exec(ctx, `DELETE FROM user_favorites WHERE user_id = $1 AND meme_id = $2`, userID, memeID)
	return err
}

func (s *PostgresStore) getByHash(ctx context.Context, contentHash string) (Meme, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.content_hash = $1
		GROUP BY m.id
		LIMIT 1
	`, strings.TrimSpace(contentHash))
	if err != nil {
		return Meme{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Meme{}, os.ErrNotExist
	}
	meme, err := scanMemeRow(rows)
	if err != nil {
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
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
