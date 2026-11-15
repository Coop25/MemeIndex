package client

import (
	"encoding/json"
	"errors"
	"log"
	"memeindex/internal/audiobooks"
	"net/http"
	"strings"
)

const adminMergeBodyLimit = 64 << 10

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
	if errors.Is(err, audiobooks.ErrSeriesNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "series not found"})
		return
	}
	if err != nil {
		log.Printf("admin merge audiobook series failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "merge failed"})
		return
	}
	log.Printf("audiobooks: series %s merged into %s by %s (%d books)", id, into, storageActorName(r), moved)
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
		tags = []audiobooks.AdminTagUsage{}
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
