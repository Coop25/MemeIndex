package accessor

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetOrCreateMemeShare(memeID, userID string, now, expiresAt time.Time) (MemeShareState, error) {
	var share MemeShareState
	err := s.pool.QueryRow(context.Background(), `
		UPDATE memes
		SET share_generation = CASE WHEN share_expires_at > $2 THEN share_generation ELSE share_generation + 1 END,
			share_expires_at = $3,
			shared_at = CASE WHEN share_expires_at > $2 THEN shared_at ELSE $2 END,
			shared_by_user_id = CASE WHEN share_expires_at > $2 THEN shared_by_user_id ELSE $4 END
		WHERE id = $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
		RETURNING id, share_generation, shared_by_user_id, shared_at, share_expires_at
	`, strings.TrimSpace(memeID), now.UTC(), expiresAt.UTC(), strings.TrimSpace(userID)).Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemeShareState{}, os.ErrNotExist
	}
	if err != nil {
		return MemeShareState{}, err
	}
	// One link per generation: a renewed share (or two racing requests) gets
	// the link already issued rather than a second one. The no-op update makes
	// RETURNING yield the existing row on conflict.
	err = s.pool.QueryRow(context.Background(), `
		INSERT INTO meme_share_links (slug, meme_id, generation)
		VALUES ($1, $2, $3)
		ON CONFLICT (meme_id, generation) DO UPDATE SET generation = EXCLUDED.generation
		RETURNING slug
	`, uuid.NewString(), share.MemeID, share.Generation).Scan(&share.Slug)
	if err != nil {
		return MemeShareState{}, err
	}
	return share, nil
}

func (s *PostgresStore) GetMemeShareState(memeID string) (MemeShareState, error) {
	var share MemeShareState
	err := s.pool.QueryRow(context.Background(), `
		SELECT m.id, m.share_generation, m.shared_by_user_id, COALESCE(m.shared_at, 'epoch'::timestamptz), COALESCE(m.share_expires_at, 'epoch'::timestamptz), COALESCE(l.slug, '')
		FROM memes m
		LEFT JOIN meme_share_links l ON l.meme_id = m.id AND l.generation = m.share_generation
		WHERE m.id = $1 AND COALESCE(m.hidden_from_app, FALSE) = FALSE
	`, strings.TrimSpace(memeID)).Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt, &share.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemeShareState{}, os.ErrNotExist
	}
	return share, err
}

func (s *PostgresStore) ResolveMemeShareSlug(slug string) (string, int64, error) {
	var memeID string
	var generation int64
	err := s.pool.QueryRow(context.Background(), `
		SELECT meme_id, generation FROM meme_share_links WHERE slug = $1
	`, strings.TrimSpace(slug)).Scan(&memeID, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, os.ErrNotExist
	}
	return memeID, generation, err
}

func (s *PostgresStore) ListActiveMemeShares(now time.Time) ([]MemeShareState, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT m.id, m.share_generation, m.shared_by_user_id, m.shared_at, m.share_expires_at, COALESCE(l.slug, '')
		FROM memes m
		LEFT JOIN meme_share_links l ON l.meme_id = m.id AND l.generation = m.share_generation
		WHERE m.share_expires_at > $1 AND COALESCE(m.hidden_from_app, FALSE) = FALSE
		ORDER BY m.shared_at DESC
	`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := []MemeShareState{}
	for rows.Next() {
		var share MemeShareState
		if err := rows.Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt, &share.Slug); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

func (s *PostgresStore) RevokeMemeShare(memeID string) error {
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET share_generation = share_generation + 1, share_expires_at = NULL
		WHERE id = $1
	`, strings.TrimSpace(memeID))
	if err == nil && tag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return err
}

func (s *PostgresStore) RevokeAllMemeShares(now time.Time) (int, error) {
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET share_generation = share_generation + 1, share_expires_at = NULL
		WHERE share_expires_at > $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
	`, now.UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
