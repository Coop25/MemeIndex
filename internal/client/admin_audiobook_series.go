package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const adminMergeBodyLimit = 64 << 10

type adminRestrictedUser struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// adminAudiobookTagUsage is one tag used by audiobooks, for the admin Series
// & Tags page.
type adminAudiobookTagUsage struct {
	Tag             string                `json:"tag"`
	Audiobooks      int                   `json:"audiobooks"`
	Memes           int                   `json:"memes"`
	RestrictedUsers []adminRestrictedUser `json:"restricted_users"`

	tagID int64
}

// MergeSeries implements audiobookStore.
func (s *postgresAudiobookStore) MergeSeries(ctx context.Context, fromID, intoID string) (int, error) {
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
		return 0, ErrAudiobookSeriesNotFound
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

// AdminAudiobookTagUsage implements audiobookStore. Audiobook and meme counts
// come from separate aggregates so the two joins cannot multiply each other.
func (s *postgresAudiobookStore) AdminAudiobookTagUsage(ctx context.Context) ([]adminAudiobookTagUsage, error) {
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
	var out []adminAudiobookTagUsage
	index := map[int64]int{}
	for rows.Next() {
		var u adminAudiobookTagUsage
		if err := rows.Scan(&u.tagID, &u.Tag, &u.Audiobooks, &u.Memes); err != nil {
			return nil, fmt.Errorf("scan audiobook tag usage: %w", err)
		}
		u.RestrictedUsers = []adminRestrictedUser{}
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
			out[i].RestrictedUsers = append(out[i].RestrictedUsers, adminRestrictedUser{UserID: userID})
		}
	}
	if err := restr.Err(); err != nil {
		return nil, fmt.Errorf("iterate audiobook tag restrictions: %w", err)
	}
	return out, nil
}

// handleAdminAudiobookSeries serves POST /api/admin/audiobook-series/{id}/merge.
func (s *Server) handleAdminAudiobookSeries(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/audiobook-series/"), "/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || id == "" || action != "merge" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.audiobooks == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
		return
	}
	var payload struct {
		Into string `json:"into"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, adminMergeBodyLimit)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	into := strings.TrimSpace(payload.Into)
	if into == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "into is required"})
		return
	}
	if into == id {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot merge a series into itself"})
		return
	}
	moved, err := s.audiobooks.MergeSeries(r.Context(), id, into)
	if errors.Is(err, ErrAudiobookSeriesNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "series not found"})
		return
	}
	if err != nil {
		log.Printf("admin merge audiobook series failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "merge failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"moved": moved})
}

// handleAdminAudiobookTags serves GET /api/admin/audiobook-tags.
func (s *Server) handleAdminAudiobookTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.audiobooks == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
		return
	}
	tags, err := s.audiobooks.AdminAudiobookTagUsage(r.Context())
	if err != nil {
		log.Printf("admin audiobook tag usage failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "tag usage failed"})
		return
	}
	if tags == nil {
		tags = []adminAudiobookTagUsage{}
	}
	names := s.adminUserNames(r.Context())
	for i := range tags {
		for j := range tags[i].RestrictedUsers {
			u := &tags[i].RestrictedUsers[j]
			u.DisplayName = names[u.UserID]
			if u.DisplayName == "" {
				u.DisplayName = u.UserID
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}
