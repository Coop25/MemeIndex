package accessor

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"
)

// RemoveSuggestedTag drops just the one tag from the stored list, moves
// updated_at only when it removed something, and reports a missing meme.
func TestPostgresRemoveSuggestedTag(t *testing.T) {
	f := newTagMergeFixture(t)
	id := f.raceMeme()
	bird, dog, fish := f.tag("bird"), f.tag("dog"), f.tag("fish")
	if err := f.store.ReplaceSuggestedTags(context.Background(), id, []string{bird, dog, fish}); err != nil {
		t.Fatal(err)
	}
	updatedAt := func() time.Time {
		t.Helper()
		var at time.Time
		if err := f.store.pool.QueryRow(context.Background(), `SELECT updated_at FROM memes WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	before := updatedAt()
	if err := f.store.RemoveSuggestedTag(context.Background(), id, " "+dog+" "); err != nil {
		t.Fatalf("RemoveSuggestedTag: %v", err)
	}
	got, err := f.store.ListSuggestedTags(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{bird, fish}; !slices.Equal(got, want) {
		t.Fatalf("suggested tags = %v, want %v", got, want)
	}
	removedAt := updatedAt()
	if !removedAt.After(before) {
		t.Errorf("updated_at did not move when a suggestion was removed")
	}

	if err := f.store.RemoveSuggestedTag(context.Background(), id, dog); err != nil {
		t.Fatalf("RemoveSuggestedTag of an absent tag: %v", err)
	}
	if !updatedAt().Equal(removedAt) {
		t.Errorf("updated_at moved although nothing was removed")
	}

	if err := f.store.RemoveSuggestedTag(context.Background(), "missing-"+f.token, dog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing meme: err = %v, want os.ErrNotExist", err)
	}
}
