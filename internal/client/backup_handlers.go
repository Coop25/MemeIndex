package client

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}

	status, started := s.backup.startExport()
	if !started {
		writeJSON(w, http.StatusConflict, status)
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}

func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.backup.exportStatus())
}

// sanitizeDownloadFilename strips path separators and control/quote characters so
// the value is safe to place inside a Content-Disposition header even if the
// stored backup name is ever influenced by something other than our own writer.
func sanitizeDownloadFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' || r == '/' {
			return -1
		}
		return r
	}, name)
	return cleaned
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}

	// ?file= names the manifest or one part of the latest backup; without it
	// the manifest is served, since it lists every part to fetch next.
	requested := r.URL.Query().Get("file")
	if requested == "" {
		requested = s.backup.exportStatus().Filename
	}
	file, status, err := s.backup.openLatestExport(requested)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "no completed backup file by that name is available", http.StatusNotFound)
			return
		}
		log.Printf("open completed backup failed: %v", err)
		http.Error(w, "failed to open completed backup", http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		log.Printf("stat completed backup failed: %v", err)
		http.Error(w, "failed to inspect completed backup", http.StatusInternalServerError)
		return
	}

	filename := sanitizeDownloadFilename(requested)
	if filename == "" {
		filename = "memeindex-backup.tar.gz"
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = `attachment; filename="memeindex-backup.tar.gz"`
	}
	w.Header().Set("Content-Disposition", disposition)
	if requested == status.Filename {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}
	s.runBackupRestore(w, r, func(ctx context.Context) error {
		return s.backup.importArchive(ctx, r.Body)
	})
}

// runBackupRestore wraps a restore with the checks and locking every import
// path shares.
func (s *Server) runBackupRestore(w http.ResponseWriter, r *http.Request, restore func(context.Context) error) {
	if s.backup.exportStatus().State == "running" {
		http.Error(w, "wait for the running backup to finish before importing", http.StatusConflict)
		return
	}
	// Hold the process-wide maintenance lock for the whole restore. importArchive
	// truncates tables and renames the media directories, so a concurrent write
	// could land against half-restored state; maintenanceGuard turns those away
	// with 503 while this flag is set.
	if !s.restoring.CompareAndSwap(false, true) {
		http.Error(w, "a restore is already in progress", http.StatusConflict)
		return
	}
	defer s.restoring.Store(false)
	if err := restore(r.Context()); err != nil {
		log.Printf("backup import failed: %v", err)
		http.Error(w, "backup import failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.managers.ReloadAfterRestore(); err != nil {
		log.Printf("reload in-memory state after backup import failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Backup imported successfully.",
	})
}

// handleBackupImportParts serves the staging area for restoring a split
// backup: GET reports which parts have arrived, PUT ?file=<part name> uploads
// one part, and DELETE discards everything staged so far.
func (s *Server) handleBackupImportParts(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		status, err := s.backup.incomingStatus()
		if err != nil {
			http.Error(w, "staged backup is unreadable: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodPut:
		if s.restoring.Load() {
			http.Error(w, "a restore is already in progress", http.StatusConflict)
			return
		}
		status, err := s.backup.receiveImportPart(r.URL.Query().Get("file"), r.Body)
		if err != nil {
			log.Printf("backup part upload rejected: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodDelete:
		if err := s.backup.discardIncoming(); err != nil {
			http.Error(w, "failed to discard staged backup: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, backupIncomingStatus{})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBackupImportManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read backup manifest", http.StatusBadRequest)
		return
	}
	status, err := s.backup.stageImportManifest(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBackupImportPartsRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.backup == nil {
		http.Error(w, "portable backups require PostgreSQL storage", http.StatusConflict)
		return
	}
	s.runBackupRestore(w, r, s.backup.importStagedParts)
}
