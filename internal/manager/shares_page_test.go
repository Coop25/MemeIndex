package manager

import (
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

func newSharedMemesManager(t *testing.T, count int) (*MemeManager, time.Time) {
	t.Helper()
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		meme, err := store.Create(accessor.CreateInput{
			File:        strings.NewReader(fmt.Sprintf("bytes %d", i)),
			Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
			Filename:    fmt.Sprintf("meme-%02d.png", i),
			ContentType: "image/png",
		})
		if err != nil {
			t.Fatal(err)
		}
		// Later memes are shared later, so the newest share is the last one.
		sharedAt := now.Add(time.Duration(i) * time.Minute)
		if _, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", sharedAt, sharedAt.Add(MemeShareLifetime)); err != nil {
			t.Fatal(err)
		}
	}
	return NewMemeManager(store), now.Add(time.Duration(count) * time.Minute)
}

func TestListActiveMemeSharesPagePagesNewestFirst(t *testing.T) {
	m, now := newSharedMemesManager(t, 7)

	first, total, offset, limit, err := m.ListActiveMemeSharesPage(now, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || offset != 0 || limit != 3 || len(first) != 3 {
		t.Fatalf("first page: total=%d offset=%d limit=%d len=%d", total, offset, limit, len(first))
	}
	if first[0].Meme.OriginalName != "meme-06.png" || first[2].Meme.OriginalName != "meme-04.png" {
		t.Fatalf("first page order = %s .. %s", first[0].Meme.OriginalName, first[2].Meme.OriginalName)
	}

	last, _, offset, _, err := m.ListActiveMemeSharesPage(now, 6, 3)
	if err != nil {
		t.Fatal(err)
	}
	if offset != 6 || len(last) != 1 || last[0].Meme.OriginalName != "meme-00.png" {
		t.Fatalf("last page: offset=%d len=%d", offset, len(last))
	}
}

func TestListActiveMemeSharesPageClampsItsArguments(t *testing.T) {
	m, now := newSharedMemesManager(t, 7)

	// Past the end: pulled back to the last page rather than returning nothing.
	page, total, offset, _, err := m.ListActiveMemeSharesPage(now, 30, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || offset != 6 || len(page) != 1 {
		t.Fatalf("past-the-end page: total=%d offset=%d len=%d", total, offset, len(page))
	}

	page, _, offset, limit, err := m.ListActiveMemeSharesPage(now, -5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 || limit != activeSharesPageDefaultLimit || len(page) != 7 {
		t.Fatalf("defaults: offset=%d limit=%d len=%d", offset, limit, len(page))
	}

	_, _, _, limit, err = m.ListActiveMemeSharesPage(now, 0, 100000)
	if err != nil || limit != activeSharesPageMaxLimit {
		t.Fatalf("limit = %d, err = %v", limit, err)
	}
}

func TestListActiveMemeSharesPageWithNoShares(t *testing.T) {
	m, now := newSharedMemesManager(t, 0)
	page, total, offset, _, err := m.ListActiveMemeSharesPage(now, 50, 25)
	if err != nil || total != 0 || offset != 0 || len(page) != 0 {
		t.Fatalf("empty: total=%d offset=%d len=%d err=%v", total, offset, len(page), err)
	}
}

func TestRetryOrDropTagSuggestionGivesUpAfterCap(t *testing.T) {
	m := newLifecycleManager(t)
	if !m.enqueueTagSuggestion("meme-1") {
		t.Fatal("enqueue failed")
	}
	if id, ok := m.dequeueTagSuggestion(); !ok || id != "meme-1" {
		t.Fatalf("dequeue = %q, %v", id, ok)
	}

	failure := errors.New("model timed out")
	for attempt := 1; attempt < maxTagSuggestionFailures; attempt++ {
		if !m.retryOrDropTagSuggestion("meme-1", failure) {
			t.Fatalf("attempt %d: dropped before the cap", attempt)
		}
		if id, ok := m.dequeueTagSuggestion(); !ok || id != "meme-1" {
			t.Fatalf("attempt %d: meme was not requeued", attempt)
		}
	}
	if m.retryOrDropTagSuggestion("meme-1", failure) {
		t.Fatal("meme was requeued past the cap")
	}

	status := m.TagSuggestionQueueStatus(0, 10)
	if status.QueueLength != 0 || !strings.Contains(status.LastError, "gave up on meme-1") {
		t.Fatalf("queue length = %d, last error = %q", status.QueueLength, status.LastError)
	}
	// Dropped, not blacklisted: it can be queued again later.
	if !m.enqueueTagSuggestion("meme-1") {
		t.Fatal("a dropped meme could not be queued again")
	}
}
