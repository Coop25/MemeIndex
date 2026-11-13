package accessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			ID:            item.ID,
			OriginalName:  item.OriginalName,
			StoredName:    item.StoredName,
			FilePath:      item.FilePath,
			ContentType:   item.ContentType,
			SizeBytes:     item.SizeBytes,
			Tags:          normalizeTags(item.Tags),
			SuggestedTags: normalizeTags(item.SuggestedTags),
			Notes:         strings.TrimSpace(item.Notes),
			CreatedAt:     item.CreatedAt,
			UpdatedAt:     item.UpdatedAt,
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
