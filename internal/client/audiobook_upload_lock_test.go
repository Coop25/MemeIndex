package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"memeindex/internal/audiobooks"
)

// uploadLockStore is just enough of the audiobook store for the upload
// routes: one known upload id, "known", whose complete can be made to fail;
// every other id does not exist.
type uploadLockStore struct {
	audiobooks.Store
	completeErr error
	completed   chan struct{}
}

func (s *uploadLockStore) GetUpload(_ context.Context, id, _ string) (audiobooks.Upload, error) {
	if id != "known" {
		return audiobooks.Upload{}, audiobooks.ErrUploadNotFound
	}
	return audiobooks.Upload{ID: id, TotalBytes: 10, TempPath: "missing.part"}, nil
}

func (s *uploadLockStore) AbortUpload(_ context.Context, id, _ string) (audiobooks.Upload, error) {
	if id != "known" {
		return audiobooks.Upload{}, audiobooks.ErrUploadNotFound
	}
	return audiobooks.Upload{ID: id}, nil
}

func (s *uploadLockStore) CompleteUpload(_ context.Context, id, _ string, _ float64) (audiobooks.Record, error) {
	if s.completed != nil {
		close(s.completed)
	}
	if id != "known" {
		return audiobooks.Record{}, audiobooks.ErrUploadNotFound
	}
	if s.completeErr != nil {
		return audiobooks.Record{}, s.completeErr
	}
	return audiobooks.Record{ID: "book", StoragePath: "book.m4b", MimeType: "audio/mp4"}, nil
}

func (s *uploadLockStore) DiscardUpload(context.Context, string) error { return nil }

func uploadRequest(t *testing.T, s *Server, method, path string, body string, header map[string]string) int {
	t.Helper()
	r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.handleAudiobookUploadByID(w, r)
	return w.Code
}

// Requests for upload ids that don't exist - and uploads that fail to
// complete, are aborted or are reclaimed - must not leave lock entries
// behind; before, every id ever seen in a URL stayed in the map for good.
func TestAudiobookUploadLocksDoNotAccumulate(t *testing.T) {
	store := &uploadLockStore{completeErr: fmt.Errorf("%w: received 3 of 10 bytes", audiobooks.ErrUploadIncomplete)}
	s := &Server{audiobooks: store}
	before := audiobookUploadLockCount()

	for i := range 50 {
		id := "bogus-" + strings.Repeat("x", i)
		if code := uploadRequest(t, s, http.MethodPatch, "/api/audiobooks/uploads/"+id, "abc", map[string]string{"Upload-Offset": "0"}); code != http.StatusNotFound {
			t.Fatalf("PATCH unknown upload: got %d, want 404", code)
		}
		if code := uploadRequest(t, s, http.MethodDelete, "/api/audiobooks/uploads/"+id, "", nil); code != http.StatusNotFound {
			t.Fatalf("DELETE unknown upload: got %d, want 404", code)
		}
		if code := uploadRequest(t, s, http.MethodPost, "/api/audiobooks/uploads/"+id+"/complete", "", nil); code != http.StatusNotFound {
			t.Fatalf("complete unknown upload: got %d, want 404", code)
		}
	}
	if code := uploadRequest(t, s, http.MethodPost, "/api/audiobooks/uploads/known/complete", "", nil); code != http.StatusConflict {
		t.Fatalf("failed complete: got %d, want 409", code)
	}
	if code := uploadRequest(t, s, http.MethodDelete, "/api/audiobooks/uploads/known", "", nil); code != http.StatusNoContent {
		t.Fatalf("abort: got %d, want 204", code)
	}
	if err := s.discardAudiobookUpload(context.Background(), audiobooks.Upload{ID: "stale", TempPath: "missing.part"}); err != nil {
		t.Fatalf("discard: %v", err)
	}

	if got := audiobookUploadLockCount(); got != before {
		t.Fatalf("upload lock entries = %d after the requests, want %d", got, before)
	}
}

// Completing an upload must still wait for chunk writes already in flight,
// and the entry must survive until both are done so the two never end up on
// different mutexes.
func TestAudiobookUploadCompleteWaitsForInFlightChunk(t *testing.T) {
	store := &uploadLockStore{completed: make(chan struct{})}
	s := &Server{audiobooks: store, audiobookRemuxProgress: newAudiobookRemuxProgress()}

	releaseChunk := lockAudiobookUploadShared("known")
	done := make(chan int, 1)
	go func() {
		done <- uploadRequest(t, s, http.MethodPost, "/api/audiobooks/uploads/known/complete", "", nil)
	}()

	select {
	case <-store.completed:
		t.Fatal("complete ran while a chunk write still held the upload")
	case <-time.After(100 * time.Millisecond):
	}
	if got := audiobookUploadLockCount(); got != 1 {
		t.Fatalf("lock entries while the chunk and complete share it = %d, want 1", got)
	}

	releaseChunk()
	select {
	case code := <-done:
		if code != http.StatusCreated {
			t.Fatalf("complete: got %d, want 201", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("complete never ran after the chunk finished")
	}
	if got := audiobookUploadLockCount(); got != 0 {
		t.Fatalf("lock entries after complete = %d, want 0", got)
	}
}

// Every 409 from complete used to look the same to the browser, which took
// each one for a duplicate: a file that was not audio was reported as
// "already exists", and an incomplete upload that could be resumed was
// abandoned. Each now carries a code the client can act on, next to the
// message it shows.
func TestCompleteAudiobookUploadConflictCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{audiobooks.ErrDuplicateTitle, "duplicate"},
		{audiobooks.ErrNotAudio, "not_audio"},
		{fmt.Errorf("%w: received 4 of 10 bytes", audiobooks.ErrUploadIncomplete), "incomplete"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			s := &Server{audiobooks: &uploadLockStore{completeErr: tc.err}}
			w := httptest.NewRecorder()
			s.handleAudiobookUploadByID(w, httptest.NewRequest(http.MethodPost, "https://example.com/api/audiobooks/uploads/known/complete", strings.NewReader("")))
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409", w.Code)
			}
			var body struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", w.Body.String(), err)
			}
			if body.Code != tc.code {
				t.Fatalf("code = %q, want %q", body.Code, tc.code)
			}
			if body.Error != tc.err.Error() {
				t.Fatalf("error = %q, want %q", body.Error, tc.err.Error())
			}
		})
	}
}
