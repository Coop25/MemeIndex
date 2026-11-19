package audiobooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The library lists books grouped by series, and the groups come out in the
// order the series were created. Series IDs used to be time-ordered UUIDv6s
// ("01f1...") and are UUIDv7s ("01a1...") now, so an instance upgraded across
// that change has both. Ordering the groups by the ID itself put every series
// created after the upgrade ahead of all the older ones.
func TestListAudiobooksOrdersSeriesGroupsByCreationAcrossIDFormats(t *testing.T) {
	store := newTestAudiobookStore(t)
	ctx := context.Background()
	token := uuid.NewString()
	tag := "series-order-" + token

	created := time.Now().Add(-time.Hour).UTC()
	// A v6-style ID from before the change, and a v7 one made after it.
	olderSeries := "01f0c5a2-0000-6000-8000-" + token[24:]
	newerSeries := "01a1f4bd-0000-7000-8000-" + token[24:]
	for i, id := range []string{olderSeries, newerSeries} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO audiobook_series (id, name, created_at) VALUES ($1, $2, $3)
		`, id, "Series "+id, created.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert series: %v", err)
		}
		t.Cleanup(func() {
			_, _ = store.pool.Exec(context.Background(), `DELETE FROM audiobook_series WHERE id = $1`, id)
		})
	}

	// The newer series' book goes in first, so neither the books' own
	// creation order nor their IDs line up with the expected order by chance.
	bookInNewer, _ := insertTestAudiobook(t, store, 100)
	bookInOlder, _ := insertTestAudiobook(t, store, 100)
	for book, series := range map[string]string{bookInNewer: newerSeries, bookInOlder: olderSeries} {
		seq := 1.0
		if _, err := store.SetAudiobookSeries(ctx, book, &series, &seq); err != nil {
			t.Fatalf("set series: %v", err)
		}
		if _, err := store.ReplaceAudiobookTags(ctx, book, []string{tag}); err != nil {
			t.Fatalf("tag book: %v", err)
		}
	}

	items, err := store.ListAudiobooks(ctx, "", ListFilter{Tag: tag})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("listed %d books, want 2", len(items))
	}
	if items[0].ID != bookInOlder || items[1].ID != bookInNewer {
		t.Fatalf("series groups listed newer first: got %s then %s, want the older series' book %s first",
			items[0].ID, items[1].ID, bookInOlder)
	}
}
