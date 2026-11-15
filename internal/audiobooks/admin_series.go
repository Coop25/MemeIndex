package audiobooks

import (
	"context"
	"fmt"
)

type AdminRestrictedUser struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// AdminTagUsage is one tag used by audiobooks, for the admin Series
// & Tags page.
type AdminTagUsage struct {
	Tag             string                `json:"tag"`
	Audiobooks      int                   `json:"audiobooks"`
	Memes           int                   `json:"memes"`
	RestrictedUsers []AdminRestrictedUser `json:"restricted_users"`

	tagID int64
}

// MergeSeries implements Store.
func (s *PostgresStore) MergeSeries(ctx context.Context, fromID, intoID string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin merge series: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var found int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM audiobook_series WHERE id = ANY($1)`, []string{fromID, intoID}).Scan(&found); err != nil {
		return 0, fmt.Errorf("check series to merge: %w", err)
	}
	if found != 2 {
		return 0, ErrSeriesNotFound
	}
	tag, err := tx.Exec(ctx, `UPDATE audiobooks SET series_id = $2 WHERE series_id = $1`, fromID, intoID)
	if err != nil {
		return 0, fmt.Errorf("move audiobooks between series: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM audiobook_series WHERE id = $1`, fromID); err != nil {
		return 0, fmt.Errorf("delete merged series: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit merge series: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// AdminAudiobookTagUsage implements Store. Audiobook and meme counts
// come from separate aggregates so the two joins cannot multiply each other.
func (s *PostgresStore) AdminAudiobookTagUsage(ctx context.Context) ([]AdminTagUsage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, ab.n, COALESCE(mt.n, 0)
		FROM (SELECT tag_id, count(*) AS n FROM audiobook_tags GROUP BY tag_id) ab
		JOIN tags t ON t.id = ab.tag_id
		LEFT JOIN (SELECT tag_id, count(*) AS n FROM meme_tags GROUP BY tag_id) mt ON mt.tag_id = ab.tag_id
		ORDER BY ab.n DESC, t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("query audiobook tag usage: %w", err)
	}
	defer rows.Close()
	var out []AdminTagUsage
	index := map[int64]int{}
	for rows.Next() {
		var u AdminTagUsage
		if err := rows.Scan(&u.tagID, &u.Tag, &u.Audiobooks, &u.Memes); err != nil {
			return nil, fmt.Errorf("scan audiobook tag usage: %w", err)
		}
		u.RestrictedUsers = []AdminRestrictedUser{}
		index[u.tagID] = len(out)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobook tag usage: %w", err)
	}
	rows.Close()

	restr, err := s.pool.Query(ctx, `SELECT tag_id, user_id FROM app_user_audiobook_tag_restrictions ORDER BY user_id`)
	if err != nil {
		return nil, fmt.Errorf("query audiobook tag restrictions: %w", err)
	}
	defer restr.Close()
	for restr.Next() {
		var tagID int64
		var userID string
		if err := restr.Scan(&tagID, &userID); err != nil {
			return nil, fmt.Errorf("scan audiobook tag restriction: %w", err)
		}
		if i, ok := index[tagID]; ok {
			out[i].RestrictedUsers = append(out[i].RestrictedUsers, AdminRestrictedUser{UserID: userID})
		}
	}
	if err := restr.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobook tag restrictions: %w", err)
	}
	return out, nil
}
