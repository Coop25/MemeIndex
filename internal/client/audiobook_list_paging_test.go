package client

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestListAudiobooksPaging covers that Limit/Offset walk the list in its
// usual order with no book repeated or skipped across pages, and that Limit
// 0 still returns everything. The books share one throwaway tag so other
// rows in the test database don't leak into the result.
func TestListAudiobooksPaging(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	tag := "paging-" + uuid.Must(uuid.NewV6()).String()

	const total = 7
	for range total {
		id, _ := insertTestAudiobook(t, store, 100)
		if _, err := store.ReplaceAudiobookTags(ctx, id, []string{tag}); err != nil {
			t.Fatalf("tag book: %v", err)
		}
	}

	all, err := store.ListAudiobooks(ctx, "", audiobookListFilter{Tag: tag})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != total {
		t.Fatalf("unlimited list has %d books, want %d", len(all), total)
	}

	var paged []string
	for offset := 0; offset < total; offset += 3 {
		page, err := store.ListAudiobooks(ctx, "", audiobookListFilter{Tag: tag, Limit: 3, Offset: offset})
		if err != nil {
			t.Fatalf("list page at %d: %v", offset, err)
		}
		if want := min(3, total-offset); len(page) != want {
			t.Fatalf("page at %d has %d books, want %d", offset, len(page), want)
		}
		for _, item := range page {
			paged = append(paged, item.ID)
		}
	}
	for i, item := range all {
		if paged[i] != item.ID {
			t.Fatalf("paged order differs at %d: got %s, want %s", i, paged[i], item.ID)
		}
	}
}
