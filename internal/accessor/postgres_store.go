package accessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool       *pgxpool.Pool
	uploadDir  string
	previewDir string
	dataDir    string
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

	pool, err := pgxpool.New(ctx, databaseURL)
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

func (s *PostgresStore) ThumbnailDir() string {
	return s.previewDir
}

func (s *PostgresStore) EnsurePreviewAssets() error {
	memes := s.List("", "", false, "")
	totalVideos := 0
	generated := 0
	existing := 0
	failed := 0

	for _, meme := range memes {
		if strings.HasPrefix(meme.ContentType, "video/") {
			totalVideos += 1
		}
	}

	if totalVideos == 0 {
		log.Printf("preview asset backfill: no video memes found")
		return nil
	}

	log.Printf("preview asset backfill: starting postgres video thumbnails for %d video(s)", totalVideos)

	processedVideos := 0
	for i := range memes {
		if !strings.HasPrefix(memes[i].ContentType, "video/") {
			continue
		}

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

func (s *PostgresStore) List(userID, query string, favoritesOnly bool, tag string) []Meme {
	ctx := context.Background()
	userID = normalizeFavoriteUserID(userID)
	query = strings.ToLower(strings.TrimSpace(query))
	tag = normalizeTag(tag)

	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			m.notes,
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
		WHERE
			(
				$2 = '' OR
				LOWER(m.original_name) LIKE '%' || $2 || '%' OR
				LOWER(m.notes) LIKE '%' || $2 || '%' OR
				EXISTS (
					SELECT 1
					FROM meme_tags mtq
					JOIN tags tq ON tq.id = mtq.tag_id
					WHERE mtq.meme_id = m.id AND tq.name LIKE '%' || $2 || '%'
				)
			)
			AND
			(
				$3 = '' OR
				EXISTS (
					SELECT 1
					FROM meme_tags mtt
					JOIN tags tt ON tt.id = mtt.tag_id
					WHERE mtt.meme_id = m.id AND tt.name LIKE '%' || $3 || '%'
				)
			)
			AND
			(
				NOT $4 OR
				EXISTS (
					SELECT 1
					FROM user_favorites uff
					WHERE uff.user_id = $1 AND uff.meme_id = m.id
				)
			)
		GROUP BY m.id
		ORDER BY m.created_at DESC
	`, userID, query, tag, favoritesOnly)
	if err != nil {
		return nil
	}
	defer rows.Close()

	memes := make([]Meme, 0)
	for rows.Next() {
		meme, scanErr := scanMemeRow(rows)
		if scanErr != nil {
			return memes
		}
		decoratePreviewPath(&meme, s.previewDir)
		memes = append(memes, meme)
	}
	return memes
}

func (s *PostgresStore) SuggestTags(prefix string, limit int) []string {
	ctx := context.Background()
	prefix = normalizeTag(prefix)
	if limit <= 0 {
		limit = 8
	}

	rows, err := s.pool.Query(ctx, `
		SELECT name
		FROM tags
		WHERE $1 = '' OR name LIKE '%' || $1 || '%'
		ORDER BY name
		LIMIT $2
	`, prefix, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	tags := make([]string, 0, limit)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return tags
		}
		tags = append(tags, tag)
	}
	return tags
}

func (s *PostgresStore) GetByID(userID, id string) (Meme, error) {
	ctx := context.Background()
	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(userID), strings.TrimSpace(id))
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
		WHERE NOT (m.id = ANY($1::text[]))
		ORDER BY random()
		LIMIT 1
	`, normalizedExcluded).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			SELECT m.id
			FROM memes m
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
	ext := filepath.Ext(input.Filename)
	storedName := id + ext
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
		ID:           id,
		OriginalName: input.Filename,
		StoredName:   storedName,
		FilePath:     "/uploads/" + storedName,
		ContentType:  detectContentType(input.Header, input.ContentType, input.Filename),
		ContentHash:  contentHashString(hasher),
		SizeBytes:    size,
		Tags:         normalizeTags(input.Tags),
		Notes:        strings.TrimSpace(input.Notes),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := ensurePreviewAsset(s.uploadDir, s.previewDir, &meme); err != nil {
		// Thumbnail generation is best-effort.
	}

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	if existing, err := s.getByHash(ctx, meme.ContentHash); err == nil {
		_ = os.Remove(targetPath)
		return Meme{}, &DuplicateMemeError{Existing: existing}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO memes (
			id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, meme.Notes, meme.CreatedAt, meme.UpdatedAt); err != nil {
		_ = os.Remove(targetPath)
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			if existing, lookupErr := s.getByHash(ctx, meme.ContentHash); lookupErr == nil {
				return Meme{}, &DuplicateMemeError{Existing: existing}
			}
		}
		return Meme{}, err
	}

	if err := s.replaceTags(ctx, tx, meme.ID, meme.Tags); err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	return meme, nil
}

func (s *PostgresStore) Update(userID, id string, update MemeUpdate) (Meme, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	id = strings.TrimSpace(id)
	update.Tags = normalizeTags(update.Tags)
	update.Notes = strings.TrimSpace(update.Notes)

	commandTag, err := tx.Exec(ctx, `
		UPDATE memes
		SET notes = $2, updated_at = $3
		WHERE id = $1
	`, id, update.Notes, time.Now().UTC())
	if err != nil {
		return Meme{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return Meme{}, os.ErrNotExist
	}

	if err := s.replaceTags(ctx, tx, id, update.Tags); err != nil {
		return Meme{}, err
	}
	if err := s.setFavoriteInExecutor(ctx, tx, userID, id, update.Favorite); err != nil {
		return Meme{}, err
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

func (s *PostgresStore) Delete(id string) error {
	ctx := context.Background()
	id = strings.TrimSpace(id)
	var storedName string
	err := s.pool.QueryRow(ctx, `SELECT stored_name FROM memes WHERE id = $1`, id).Scan(&storedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return os.ErrNotExist
		}
		return err
	}

	commandTag, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}

	targetPath := filepath.Join(s.uploadDir, storedName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove upload file: %w", err)
	}
	thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(storedName))
	if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove thumbnail file: %w", err)
	}
	return nil
}

func (s *PostgresStore) LoadReelSessions() (map[string]ReelSessionRecord, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, history, position, last_activity
		FROM reel_sessions
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := map[string]ReelSessionRecord{}
	for rows.Next() {
		var (
			id           string
			historyJSON  []byte
			position     int
			lastActivity time.Time
			history      []string
		)
		if err := rows.Scan(&id, &historyJSON, &position, &lastActivity); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(historyJSON, &history); err != nil {
			return nil, err
		}
		sessions[id] = ReelSessionRecord{
			History:      history,
			Position:     position,
			LastActivity: lastActivity,
		}
	}
	return sessions, rows.Err()
}

func (s *PostgresStore) SaveReelSession(sessionID string, session ReelSessionRecord) error {
	historyJSON, err := json.Marshal(session.History)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO reel_sessions (id, history, position, last_activity)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET history = EXCLUDED.history,
			position = EXCLUDED.position,
			last_activity = EXCLUDED.last_activity
	`, strings.TrimSpace(sessionID), historyJSON, session.Position, session.LastActivity.UTC())
	return err
}

func (s *PostgresStore) DeleteReelSession(sessionID string) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM reel_sessions WHERE id = $1`, strings.TrimSpace(sessionID))
	return err
}

func (s *PostgresStore) CleanupStaleReelSessions(before time.Time) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM reel_sessions WHERE last_activity < $1`, before.UTC())
	return err
}

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS memes (
			id TEXT PRIMARY KEY,
			original_name TEXT NOT NULL,
			stored_name TEXT NOT NULL,
			file_path TEXT NOT NULL,
			content_type TEXT NOT NULL,
			content_hash TEXT,
			size_bytes BIGINT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);

		CREATE TABLE IF NOT EXISTS tags (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);

		CREATE TABLE IF NOT EXISTS meme_tags (
			meme_id TEXT NOT NULL REFERENCES memes(id) ON DELETE CASCADE,
			tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (meme_id, tag_id)
		);

		CREATE TABLE IF NOT EXISTS user_favorites (
			user_id TEXT NOT NULL,
			meme_id TEXT NOT NULL REFERENCES memes(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, meme_id)
		);

		CREATE TABLE IF NOT EXISTS reel_sessions (
			id TEXT PRIMARY KEY,
			history JSONB NOT NULL,
			position INTEGER NOT NULL,
			last_activity TIMESTAMPTZ NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name);
		CREATE INDEX IF NOT EXISTS idx_meme_tags_tag_id ON meme_tags(tag_id);
		CREATE INDEX IF NOT EXISTS idx_user_favorites_user_id ON user_favorites(user_id);
		CREATE INDEX IF NOT EXISTS idx_memes_created_at ON memes(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_reel_sessions_last_activity ON reel_sessions(last_activity);
	`)
	if err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		ALTER TABLE memes ADD COLUMN IF NOT EXISTS content_hash TEXT;
		CREATE INDEX IF NOT EXISTS idx_memes_content_hash
		ON memes(content_hash);
	`)
	if err != nil {
		return fmt.Errorf("ensure content hash schema: %w", err)
	}
	return nil
}

func (s *PostgresStore) importLegacyDataIfNeeded(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memes`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	memes, err := s.readLegacyMemes()
	if err != nil {
		return err
	}
	favoritesByUser, err := s.readLegacyFavorites()
	if err != nil {
		return err
	}
	if len(memes) == 0 && len(favoritesByUser) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, meme := range memes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO memes (
				id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO NOTHING
		`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, strings.TrimSpace(meme.Notes), meme.CreatedAt, meme.UpdatedAt); err != nil {
			return err
		}
		if err := s.replaceTags(ctx, tx, meme.ID, normalizeTags(meme.Tags)); err != nil {
			return err
		}
	}

	for userID, memeIDs := range favoritesByUser {
		normalizedUserID := normalizeFavoriteUserID(userID)
		for _, memeID := range memeIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_favorites (user_id, meme_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, normalizedUserID, strings.TrimSpace(memeID)); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) backfillMissingContentHashes(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, stored_name
		FROM memes
		WHERE content_hash IS NULL OR content_hash = ''
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type missingHash struct {
		id         string
		storedName string
	}
	var missing []missingHash
	for rows.Next() {
		var item missingHash
		if err := rows.Scan(&item.id, &item.storedName); err != nil {
			return err
		}
		missing = append(missing, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range missing {
		hash, err := computeFileHash(filepath.Join(s.uploadDir, item.storedName))
		if err != nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE memes
			SET content_hash = $2
			WHERE id = $1 AND (content_hash IS NULL OR content_hash = '')
		`, item.id, hash); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) importLegacyReelSessionsIfNeeded(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reel_sessions`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	payload, err := os.ReadFile(filepath.Join(s.dataDir, "reel_sessions.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy reel sessions: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}

	var raw map[string]ReelSessionRecord
	if err := json.Unmarshal(payload, &raw); err != nil {
		return fmt.Errorf("decode legacy reel sessions: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for sessionID, session := range raw {
		historyJSON, err := json.Marshal(session.History)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reel_sessions (id, history, position, last_activity)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
		`, strings.TrimSpace(sessionID), historyJSON, session.Position, session.LastActivity.UTC()); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) readLegacyMemes() ([]Meme, error) {
	payload, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy index: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var persisted []persistedMeme
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return nil, fmt.Errorf("decode legacy index: %w", err)
	}

	memes := make([]Meme, 0, len(persisted))
	for _, item := range persisted {
		memes = append(memes, Meme{
			ID:           item.ID,
			OriginalName: item.OriginalName,
			StoredName:   item.StoredName,
			FilePath:     item.FilePath,
			ContentType:  item.ContentType,
			SizeBytes:    item.SizeBytes,
			Tags:         normalizeTags(item.Tags),
			Notes:        strings.TrimSpace(item.Notes),
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
		})
	}

	return memes, nil
}

func (s *PostgresStore) readLegacyFavorites() (map[string][]string, error) {
	payload, err := os.ReadFile(filepath.Join(s.dataDir, "favorites.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy favorites: %w", err)
	}
	if len(payload) == 0 {
		return map[string][]string{}, nil
	}

	var raw map[string][]string
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("decode legacy favorites: %w", err)
	}
	if raw == nil {
		raw = map[string][]string{}
	}
	return raw, nil
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
			m.notes,
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

func (s *PostgresStore) replaceTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE meme_id = $1`, memeID); err != nil {
		return err
	}

	for _, tag := range normalizeTags(tags) {
		var tagID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO tags (name)
			VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, tag).Scan(&tagID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO meme_tags (meme_id, tag_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, memeID, tagID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM tags
		WHERE NOT EXISTS (
			SELECT 1
			FROM meme_tags mt
			WHERE mt.tag_id = tags.id
		)
	`); err != nil {
		return err
	}

	return nil
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
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM memes
		WHERE content_hash = $1
		LIMIT 1
	`, strings.TrimSpace(contentHash)).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}
	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(""), id)
}

func scanMemeRow(row interface{ Scan(dest ...any) error }) (Meme, error) {
	var meme Meme
	var tags []string
	if err := row.Scan(
		&meme.ID,
		&meme.OriginalName,
		&meme.StoredName,
		&meme.FilePath,
		&meme.ContentType,
		&meme.SizeBytes,
		&tags,
		&meme.Notes,
		&meme.Favorite,
		&meme.CreatedAt,
		&meme.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}
	meme.Tags = normalizeTags(tags)
	return meme, nil
}
