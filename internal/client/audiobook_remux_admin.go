package client

// TEMPORARY: the admin page that shows MP3 -> M4B audiobook remux progress -
// the one-time startup backfill plus any uploads converted since. Once every
// server has finished converting, delete this file,
// static/admin-audiobook-remux.html, the two routes registered in Routes(),
// the audiobookRemuxProgress field on Server, and its calls in
// audiobook_remux.go - nothing else depends on it.

import (
	"errors"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// audiobookRemuxProgress is an in-memory record of the current server
// process's remuxes. It starts empty on every boot; the page also shows the
// live count of MP3s still in the database, so a restart mid-run is visible.
// All methods are safe on a nil receiver, so the tracker can be dropped
// without touching the conversion loop's control flow.
type audiobookRemuxProgress struct {
	mu         sync.Mutex
	state      string // "waiting", "running", "finished", "failed"
	err        string
	startedAt  time.Time
	finishedAt time.Time
	items      []audiobookRemuxItem
}

type audiobookRemuxItem struct {
	ID         string
	Title      string
	Filename   string
	SizeBefore int64
	SizeAfter  int64
	Status     string // "pending", "converting", "done", "failed", "skipped"
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
	outputPath string
}

func newAudiobookRemuxProgress() *audiobookRemuxProgress {
	return &audiobookRemuxProgress{state: "waiting"}
}

func (p *audiobookRemuxProgress) begin(books []audiobookStoredMP3) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state, p.err, p.startedAt = "running", "", time.Now()
	for _, b := range books {
		p.addLocked(b)
	}
}

// add tracks one more book - an upload that finished converting on its own,
// outside the startup run. A book already on the list is left as it is.
func (p *audiobookRemuxProgress) add(book audiobookStoredMP3) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addLocked(book)
}

func (p *audiobookRemuxProgress) addLocked(b audiobookStoredMP3) {
	for _, item := range p.items {
		if item.ID == b.ID {
			return
		}
	}
	p.items = append(p.items, audiobookRemuxItem{ID: b.ID, Title: b.Title, Filename: b.Filename, SizeBefore: b.SizeBytes, Status: "pending"})
}

func (p *audiobookRemuxProgress) itemSkipped(id, reason string) {
	p.update(id, func(item *audiobookRemuxItem) {
		item.Status, item.Error, item.FinishedAt = "skipped", reason, time.Now()
	})
}

func (p *audiobookRemuxProgress) itemStarted(id, outputPath string) {
	p.update(id, func(item *audiobookRemuxItem) {
		item.Status, item.StartedAt, item.outputPath = "converting", time.Now(), outputPath
	})
}

func (p *audiobookRemuxProgress) itemFinished(id string, sizeAfter int64, err error) {
	p.update(id, func(item *audiobookRemuxItem) {
		item.FinishedAt, item.SizeAfter = time.Now(), sizeAfter
		item.Status = "done"
		if err != nil {
			item.Status, item.Error = "failed", err.Error()
			if errors.Is(err, errAudiobookNotMP3) {
				item.Error = "not an mp3 file"
			}
		}
	})
}

func (p *audiobookRemuxProgress) update(id string, apply func(*audiobookRemuxItem)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == id {
			apply(&p.items[i])
			return
		}
	}
}

func (p *audiobookRemuxProgress) finish(err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state, p.finishedAt = "finished", time.Now()
	if err != nil {
		p.state, p.err = "failed", err.Error()
	}
}

type audiobookRemuxItemView struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Filename   string     `json:"filename"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	SizeBefore int64      `json:"size_before"`
	SizeAfter  int64      `json:"size_after,omitempty"`
	BytesDone  int64      `json:"bytes_done"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (p *audiobookRemuxProgress) snapshot() map[string]any {
	p.mu.Lock()
	state, errText, startedAt, finishedAt := p.state, p.err, p.startedAt, p.finishedAt
	items := append([]audiobookRemuxItem(nil), p.items...)
	p.mu.Unlock()

	views := make([]audiobookRemuxItemView, 0, len(items))
	counts := map[string]int{"pending": 0, "converting": 0, "done": 0, "failed": 0, "skipped": 0}
	var bytesTotal, bytesDone int64
	for _, item := range items {
		view := audiobookRemuxItemView{
			ID: item.ID, Title: item.Title, Filename: item.Filename, Status: item.Status, Error: item.Error,
			SizeBefore: item.SizeBefore, SizeAfter: item.SizeAfter,
			StartedAt: optionalTime(item.StartedAt), FinishedAt: optionalTime(item.FinishedAt),
		}
		switch item.Status {
		case "done", "failed", "skipped":
			view.BytesDone = item.SizeBefore
		case "converting":
			// ffmpeg writes the .m4b as it goes, so its size against the MP3's
			// is a fair progress estimate (it ends a little larger, for the
			// seek index, during the final +faststart rewrite).
			if info, err := os.Stat(item.outputPath); err == nil {
				view.BytesDone = min(info.Size(), item.SizeBefore)
			}
		}
		counts[item.Status]++
		bytesTotal += item.SizeBefore
		bytesDone += view.BytesDone
		views = append(views, view)
	}

	return map[string]any{
		"state":       state,
		"error":       errText,
		"started_at":  optionalTime(startedAt),
		"finished_at": optionalTime(finishedAt),
		"counts":      counts,
		"bytes_total": bytesTotal,
		"bytes_done":  bytesDone,
		"items":       views,
	}
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Server) handleAudiobookRemuxStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.audiobooks == nil || s.audiobookRemuxProgress == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "unavailable"})
		return
	}
	payload := s.audiobookRemuxProgress.snapshot()
	// Live from the database rather than this run's list, so books left on
	// MP3 by an earlier failed or interrupted run still show up.
	remaining, err := s.audiobooks.ListStoredMP3s(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to count remaining mp3 audiobooks"})
		return
	}
	payload["mp3s_remaining"] = len(remaining)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleAudiobookRemuxPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.auth.enabled() {
		session, ok := sessionFromContext(r.Context())
		if !ok || !hasPermission(session.Permissions, permissionManageUsers) {
			http.NotFound(w, r)
			return
		}
	}
	content, err := os.ReadFile(filepath.Join("static", "admin-audiobook-remux.html"))
	if err != nil {
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	nonce := newCSPNonce()
	page := strings.ReplaceAll(string(content), "{{SCRIPT_NONCE}}", html.EscapeString(nonce))
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	// Same shape as the access-denied page's policy: one nonce-pinned inline
	// script, inline styles, and fetches back to this origin only.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}
