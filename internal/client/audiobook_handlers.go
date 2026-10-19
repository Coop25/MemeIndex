package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// audiobookUploadChunkLimit bounds a single PATCH chunk, independent of the
// overall file size. It exists to stop one request from holding an
// unbounded amount of request body open regardless of how large the whole
// audiobook is allowed to be.
const audiobookUploadChunkLimit = 64 << 20 // 64 MiB

// audiobookUploadStaleAfter is how long an upload session can go without a
// chunk before the nightly sweep treats it as abandoned and reclaims its temp
// file.
const audiobookUploadStaleAfter = 24 * time.Hour

// audiobookUploadLocks serializes writes to a single upload's temp file.
// Without it, two overlapping requests for the same upload id (a retried
// request racing the original, for example) could both append to the file at
// once and interleave their bytes.
var audiobookUploadLocks sync.Map // id -> *sync.Mutex

func lockAudiobookUpload(id string) *sync.Mutex {
	lock, _ := audiobookUploadLocks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func audiobookMaxBytes(cfg Config) int64 {
	if cfg.MaxAudiobookBytes > 0 {
		return cfg.MaxAudiobookBytes
	}
	return 20 << 30 // 20 GiB
}

func (s *Server) handleAudiobooks(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.Error(w, "audiobooks require a database", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := currentAudiobookUserID(r)
	items, err := s.audiobooks.ListAudiobooks(r.Context(), userID)
	if err != nil {
		log.Printf("list audiobooks failed: %v", err)
		http.Error(w, "failed to load audiobooks", http.StatusInternalServerError)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, audiobookWithProgressJSON(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"audiobooks": out})
}

// handleAudiobookByID serves /api/audiobooks/{id} and /api/audiobooks/{id}/progress.
func (s *Server) handleAudiobookByID(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.Error(w, "audiobooks require a database", http.StatusNotImplemented)
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/audiobooks/"), "/")
	if rest == "" || strings.HasPrefix(rest, "uploads") {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getAudiobook(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		s.deleteAudiobook(w, r, id)
	case action == "progress" && r.Method == http.MethodPut:
		s.saveAudiobookProgress(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getAudiobook(w http.ResponseWriter, r *http.Request, id string) {
	record, err := s.audiobooks.GetAudiobook(r.Context(), id)
	if errors.Is(err, ErrAudiobookNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("get audiobook failed: %v", err)
		http.Error(w, "failed to load audiobook", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, audiobookJSON(record))
}

func (s *Server) deleteAudiobook(w http.ResponseWriter, r *http.Request, id string) {
	session, _ := sessionFromContext(r.Context())
	record, err := s.audiobooks.GetAudiobook(r.Context(), id)
	if errors.Is(err, ErrAudiobookNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("get audiobook failed: %v", err)
		http.Error(w, "failed to load audiobook", http.StatusInternalServerError)
		return
	}

	isOwner := session.UserID != "" && session.UserID == record.UploadedByUserID
	if !isOwner && !hasPermission(session.Permissions, permissionManageUsers) && s.auth.enabled() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "only the uploader or an admin can delete this audiobook"})
		return
	}

	deleted, err := s.audiobooks.DeleteAudiobook(r.Context(), id)
	if err != nil {
		log.Printf("delete audiobook failed: %v", err)
		http.Error(w, "failed to delete audiobook", http.StatusInternalServerError)
		return
	}
	if err := os.Remove(deleted.StoragePath); err != nil && !os.IsNotExist(err) {
		log.Printf("delete audiobook file failed: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) saveAudiobookProgress(w http.ResponseWriter, r *http.Request, id string) {
	var payload struct {
		PositionSeconds float64 `json:"positionSeconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if payload.PositionSeconds < 0 {
		payload.PositionSeconds = 0
	}

	userID := currentAudiobookUserID(r)
	if err := s.audiobooks.SaveProgress(r.Context(), id, userID, payload.PositionSeconds); err != nil {
		log.Printf("save audiobook progress failed: %v", err)
		http.Error(w, "failed to save progress", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAudiobookUploads handles POST /api/audiobooks/uploads: starting a new
// resumable upload session.
func (s *Server) handleAudiobookUploads(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.Error(w, "audiobooks require a database", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.enforceRateLimit(w, r, s.writeLimiter, "audiobook-upload") {
		return
	}

	var payload struct {
		Filename   string `json:"filename"`
		MimeType   string `json:"mimeType"`
		Title      string `json:"title"`
		Author     string `json:"author"`
		TotalBytes int64  `json:"totalBytes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	filename := strings.TrimSpace(payload.Filename)
	if filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}
	if payload.TotalBytes <= 0 {
		http.Error(w, "totalBytes must be positive", http.StatusBadRequest)
		return
	}
	if payload.TotalBytes > audiobookMaxBytes(s.config) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file exceeds the configured audiobook size limit"})
		return
	}

	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = filename
	}

	upload, err := s.audiobooks.CreateUpload(r.Context(), audiobookUploadSeed{
		UserID:     currentAudiobookUserID(r),
		Title:      title,
		Author:     strings.TrimSpace(payload.Author),
		Filename:   filename,
		MimeType:   strings.TrimSpace(payload.MimeType),
		TotalBytes: payload.TotalBytes,
	})
	if err != nil {
		log.Printf("create audiobook upload failed: %v", err)
		http.Error(w, "failed to start upload", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, audiobookUploadJSON(upload))
}

// handleAudiobookUploadByID handles the per-upload routes:
//
//	GET    /api/audiobooks/uploads/{id}          status (how many bytes received so far)
//	PATCH  /api/audiobooks/uploads/{id}          append the next chunk (Upload-Offset header)
//	DELETE /api/audiobooks/uploads/{id}          abort and discard the partial upload
//	POST   /api/audiobooks/uploads/{id}/complete finalize into a saved audiobook
func (s *Server) handleAudiobookUploadByID(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.Error(w, "audiobooks require a database", http.StatusNotImplemented)
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/audiobooks/uploads/"), "/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getAudiobookUploadStatus(w, r, id)
	case action == "" && r.Method == http.MethodPatch:
		s.appendAudiobookUploadChunk(w, r, id)
	case action == "" && r.Method == http.MethodDelete:
		s.abortAudiobookUpload(w, r, id)
	case action == "complete" && r.Method == http.MethodPost:
		s.completeAudiobookUpload(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getAudiobookUploadStatus(w http.ResponseWriter, r *http.Request, id string) {
	upload, err := s.audiobooks.GetUpload(r.Context(), id, currentAudiobookUserID(r))
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("get audiobook upload failed: %v", err)
		http.Error(w, "failed to load upload", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, audiobookUploadJSON(upload))
}

func (s *Server) appendAudiobookUploadChunk(w http.ResponseWriter, r *http.Request, id string) {
	if s.enforceRateLimit(w, r, s.writeLimiter, "audiobook-upload") {
		return
	}

	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		http.Error(w, "a valid Upload-Offset header is required", http.StatusBadRequest)
		return
	}

	userID := currentAudiobookUserID(r)
	lock := lockAudiobookUpload(id)
	lock.Lock()
	defer lock.Unlock()

	upload, err := s.audiobooks.GetUpload(r.Context(), id, userID)
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("get audiobook upload failed: %v", err)
		http.Error(w, "failed to load upload", http.StatusInternalServerError)
		return
	}
	if offset != upload.ReceivedBytes {
		writeJSON(w, http.StatusConflict, audiobookUploadJSON(upload))
		return
	}

	remaining := upload.TotalBytes - upload.ReceivedBytes
	chunkLimit := remaining
	if chunkLimit > audiobookUploadChunkLimit {
		chunkLimit = audiobookUploadChunkLimit
	}

	file, err := os.OpenFile(upload.TempPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("open audiobook upload temp file failed: %v", err)
		http.Error(w, "failed to write chunk", http.StatusInternalServerError)
		return
	}
	written, copyErr := io.Copy(file, http.MaxBytesReader(w, r.Body, chunkLimit))
	closeErr := file.Close()
	if copyErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(copyErr, &tooLarge) {
			if truncErr := os.Truncate(upload.TempPath, upload.ReceivedBytes); truncErr != nil {
				log.Printf("truncate audiobook upload temp file failed: %v", truncErr)
			}
			http.Error(w, "chunk exceeds the remaining upload size", http.StatusRequestEntityTooLarge)
			return
		}
		log.Printf("write audiobook chunk failed: %v", copyErr)
		if truncErr := os.Truncate(upload.TempPath, upload.ReceivedBytes); truncErr != nil {
			log.Printf("truncate audiobook upload temp file failed: %v", truncErr)
		}
		http.Error(w, "failed to write chunk", http.StatusInternalServerError)
		return
	}
	if closeErr != nil {
		log.Printf("close audiobook upload temp file failed: %v", closeErr)
		http.Error(w, "failed to write chunk", http.StatusInternalServerError)
		return
	}
	if written == 0 {
		http.Error(w, "chunk body was empty", http.StatusBadRequest)
		return
	}

	updated, err := s.audiobooks.AppendUploadChunk(r.Context(), id, userID, offset, written)
	if errors.Is(err, ErrAudiobookUploadOffsetConflict) {
		// Another request already advanced this upload (a racing retry). The
		// bytes we just appended are now beyond the recorded offset and would
		// corrupt the file, so drop them back to the last known-good length.
		if truncErr := os.Truncate(upload.TempPath, upload.ReceivedBytes); truncErr != nil {
			log.Printf("truncate audiobook upload temp file failed: %v", truncErr)
		}
		current, getErr := s.audiobooks.GetUpload(r.Context(), id, userID)
		if getErr != nil {
			http.Error(w, "upload offset conflict", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusConflict, audiobookUploadJSON(current))
		return
	}
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("advance audiobook upload failed: %v", err)
		http.Error(w, "failed to record chunk", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, audiobookUploadJSON(updated))
}

func (s *Server) completeAudiobookUpload(w http.ResponseWriter, r *http.Request, id string) {
	var payload struct {
		DurationSeconds float64 `json:"durationSeconds"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}

	lock := lockAudiobookUpload(id)
	lock.Lock()
	defer lock.Unlock()

	userID := currentAudiobookUserID(r)
	record, err := s.audiobooks.CompleteUpload(r.Context(), id, userID, payload.DurationSeconds)
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	audiobookUploadLocks.Delete(id)

	writeJSON(w, http.StatusCreated, audiobookJSON(record))
}

func (s *Server) abortAudiobookUpload(w http.ResponseWriter, r *http.Request, id string) {
	lock := lockAudiobookUpload(id)
	lock.Lock()
	defer lock.Unlock()

	upload, err := s.audiobooks.AbortUpload(r.Context(), id, currentAudiobookUserID(r))
	if errors.Is(err, ErrAudiobookUploadNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("abort audiobook upload failed: %v", err)
		http.Error(w, "failed to abort upload", http.StatusInternalServerError)
		return
	}
	audiobookUploadLocks.Delete(upload.ID)
	w.WriteHeader(http.StatusNoContent)
}

// startAudiobookUploadCleanupSweeper reclaims temp files for uploads that went
// quiet (a closed tab, a device that never came back) instead of leaving
// multi-gigabyte partial files on disk forever.
func (s *Server) startAudiobookUploadCleanupSweeper() {
	if s.audiobooks == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			stale, err := s.audiobooks.StaleUploads(context.Background(), time.Now().Add(-audiobookUploadStaleAfter))
			if err != nil {
				log.Printf("list stale audiobook uploads failed: %v", err)
				continue
			}
			for _, upload := range stale {
				if err := os.Remove(upload.TempPath); err != nil && !os.IsNotExist(err) {
					log.Printf("remove stale audiobook upload temp file failed: %v", err)
					continue
				}
				if err := s.audiobooks.DiscardUpload(context.Background(), upload.ID); err != nil {
					log.Printf("discard stale audiobook upload failed: %v", err)
					continue
				}
				audiobookUploadLocks.Delete(upload.ID)
				log.Printf("audiobooks: reclaimed stale upload %s (%d/%d bytes)", upload.ID, upload.ReceivedBytes, upload.TotalBytes)
			}
		}
	}()
}

func (s *Server) withAudiobookMediaAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setProtectedAssetHeaders(w)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !s.auth.enabled() {
			next.ServeHTTP(w, r)
			return
		}

		session, ok := s.auth.sessionFromRequest(r)
		if !ok || !hasPermission(session.Permissions, permissionViewAudiobooks) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
}

func (s *Server) handleAudiobookMedia(w http.ResponseWriter, r *http.Request) {
	if s.audiobooks == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/audiobook-media/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	record, err := s.audiobooks.GetAudiobook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(record.StoragePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to read audiobook", http.StatusInternalServerError)
		return
	}

	contentType := record.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, record.Filename, info.ModTime(), file)
}

func currentAudiobookUserID(r *http.Request) string {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return ""
	}
	return session.UserID
}

func audiobookJSON(record audiobookRecord) map[string]any {
	return map[string]any{
		"id":               record.ID,
		"title":            record.Title,
		"author":           record.Author,
		"filename":         record.Filename,
		"mime_type":        record.MimeType,
		"size_bytes":       record.SizeBytes,
		"duration_seconds": record.DurationSeconds,
		"uploaded_by":      record.UploadedByUserID,
		"created_at":       record.CreatedAt,
		"stream_url":       "/audiobook-media/" + record.ID,
	}
}

func audiobookWithProgressJSON(item audiobookWithProgress) map[string]any {
	payload := audiobookJSON(item.audiobookRecord)
	payload["position_seconds"] = item.PositionSeconds
	if !item.ProgressAt.IsZero() {
		payload["progress_updated_at"] = item.ProgressAt
	}
	return payload
}

func audiobookUploadJSON(upload audiobookUpload) map[string]any {
	return map[string]any{
		"upload_id":      upload.ID,
		"filename":       upload.Filename,
		"title":          upload.Title,
		"author":         upload.Author,
		"total_bytes":    upload.TotalBytes,
		"received_bytes": upload.ReceivedBytes,
	}
}
