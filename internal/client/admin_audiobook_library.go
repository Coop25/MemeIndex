package client

import (
	"log"
	"memeindex/internal/audiobooks"
	"net/http"
	"strconv"
	"strings"
)

const (
	adminAudiobookDefaultPageSize = 50
	adminAudiobookMaxPageSize     = 200
	// adminAudiobookMaxPage keeps (page-1)*pageSize far from int overflow.
	adminAudiobookMaxPage = 1_000_000
)

func (s *Server) handleAdminAudiobooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.audiobooks == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
		return
	}
	params := r.URL.Query()
	filter := params.Get("filter")
	if !audiobooks.ValidAdminFilter(filter) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown filter"})
		return
	}
	page, _ := strconv.Atoi(params.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > adminAudiobookMaxPage {
		page = adminAudiobookMaxPage
	}
	pageSize, _ := strconv.Atoi(params.Get("page_size"))
	if pageSize < 1 {
		pageSize = adminAudiobookDefaultPageSize
	}
	if pageSize > adminAudiobookMaxPageSize {
		pageSize = adminAudiobookMaxPageSize
	}
	query := audiobooks.AdminQuery{
		Sort:   params.Get("sort"),
		Dir:    strings.ToLower(params.Get("dir")),
		Filter: filter,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}
	if filter == "conversion-failed" || filter == "not-converted" {
		ids, err := s.failedRemuxAudiobookIDs(r.Context())
		if err != nil {
			log.Printf("admin audiobook failed conversions failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audiobook list failed"})
			return
		}
		query.FailedIDs = ids
	}
	items, total, err := s.audiobooks.AdminListAudiobooks(r.Context(), query)
	if err != nil {
		log.Printf("admin audiobook list failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audiobook list failed"})
		return
	}
	names := s.adminUserNames(r.Context())
	for i := range items {
		name := names[items[i].UploaderID]
		if name == "" {
			name = items[i].UploaderID
		}
		items[i].UploaderName = name
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}
