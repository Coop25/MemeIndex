package accessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

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
	defer func() { _ = tx.Rollback(ctx) }()

	for _, meme := range memes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO memes (
				id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, suggested_tags, auto_suggest_disabled, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO NOTHING
		`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, strings.TrimSpace(meme.Notes), strings.TrimSpace(meme.SourceURL), normalizeTags(meme.SuggestedTags), meme.AutoSuggestDisabled, meme.CreatedAt, meme.UpdatedAt); err != nil {
			return err
		}
		if err := s.replaceTags(ctx, tx, meme.ID, normalizeTags(meme.Tags)); err != nil {
			return err
		}
		if err := importLegacyShare(ctx, tx, meme); err != nil {
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

// importLegacyShare carries a JSON-store meme's share state into its row and
// share-link history. Share tokens are an HMAC over the meme id and share
// generation, so keeping the generation (and the link codes issued for each
// one) keeps every link the instance has already handed out working.
func importLegacyShare(ctx context.Context, tx pgx.Tx, meme Meme) error {
	if meme.ShareGeneration <= 0 && len(meme.ShareSlugs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE memes
		SET share_generation = $2, share_expires_at = $3, shared_at = $4, shared_by_user_id = $5
		WHERE id = $1
	`, meme.ID, meme.ShareGeneration, legacyTimestamp(meme.ShareExpiresAt), legacyTimestamp(meme.SharedAt), strings.TrimSpace(meme.SharedByUserID)); err != nil {
		return fmt.Errorf("import legacy share for %s: %w", meme.ID, err)
	}
	for generation, slug := range meme.ShareSlugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO meme_share_links (slug, meme_id, generation)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, slug, meme.ID, generation); err != nil {
			return fmt.Errorf("import legacy share link for %s: %w", meme.ID, err)
		}
	}
	return nil
}

// legacyTimestamp maps the JSON store's zero time ("never") to NULL.
func legacyTimestamp(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
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
	defer func() { _ = tx.Rollback(ctx) }()

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

// persistedMeme is one entry of the JSON store's index.json. The JSON store
// itself is gone; this is the format an upgrading instance's index.json still
// has. (Despite omitempty, zero times were written out as
// "0001-01-01T00:00:00Z".)
type persistedMeme struct {
	ID                  string           `json:"id"`
	OriginalName        string           `json:"originalName"`
	StoredName          string           `json:"storedName"`
	FilePath            string           `json:"filePath"`
	ContentType         string           `json:"contentType"`
	ContentHash         string           `json:"contentHash,omitempty"`
	SizeBytes           int64            `json:"sizeBytes"`
	Tags                []string         `json:"tags"`
	SuggestedTags       []string         `json:"suggestedTags,omitempty"`
	AutoSuggestDisabled bool             `json:"autoSuggestDisabled,omitempty"`
	Notes               string           `json:"notes"`
	SourceURL           string           `json:"sourceUrl,omitempty"`
	CreatedAt           time.Time        `json:"createdAt"`
	UpdatedAt           time.Time        `json:"updatedAt"`
	ShareGeneration     int64            `json:"shareGeneration,omitempty"`
	ShareExpiresAt      time.Time        `json:"shareExpiresAt,omitempty"`
	SharedAt            time.Time        `json:"sharedAt,omitempty"`
	SharedByUserID      string           `json:"sharedByUserId,omitempty"`
	ShareSlugs          map[int64]string `json:"shareSlugs,omitempty"`
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
		// Every field the JSON store persisted is carried over; dropping one
		// here loses it for good, because this import runs only once.
		memes = append(memes, Meme{
			ID:                  item.ID,
			OriginalName:        item.OriginalName,
			StoredName:          item.StoredName,
			FilePath:            item.FilePath,
			ContentType:         item.ContentType,
			ContentHash:         item.ContentHash,
			SizeBytes:           item.SizeBytes,
			Tags:                normalizeTags(item.Tags),
			SuggestedTags:       normalizeTags(item.SuggestedTags),
			AutoSuggestDisabled: item.AutoSuggestDisabled,
			Notes:               strings.TrimSpace(item.Notes),
			SourceURL:           strings.TrimSpace(item.SourceURL),
			CreatedAt:           item.CreatedAt,
			UpdatedAt:           item.UpdatedAt,
			ShareGeneration:     item.ShareGeneration,
			ShareExpiresAt:      item.ShareExpiresAt,
			SharedAt:            item.SharedAt,
			SharedByUserID:      item.SharedByUserID,
			ShareSlugs:          item.ShareSlugs,
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
