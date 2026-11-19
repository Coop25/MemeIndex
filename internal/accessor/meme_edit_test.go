package accessor

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPlanMemeEditDeltaReportsOnlyRealChanges(t *testing.T) {
	current := Meme{Tags: []string{"cat", "dog"}, Notes: "  hi ", Favorite: true}
	notes := "hi"
	favorite := true
	change, err := PlanMemeEdit(current, MemeEdit{
		TagsAdded:   []string{"Cat", " fish ", "bird"},
		TagsRemoved: []string{"dog", "absent", "bird"},
		Notes:       &notes,
		Favorite:    &favorite,
	})
	if err != nil {
		t.Fatal(err)
	}
	// "cat" is already there, "absent" is not there, and "bird" is both
	// added and removed, so the add wins.
	if got := strings.Join(change.TagsAdded, ","); got != "bird,fish" {
		t.Fatalf("TagsAdded = %q, want bird,fish", got)
	}
	if got := strings.Join(change.TagsRemoved, ","); got != "dog" {
		t.Fatalf("TagsRemoved = %q, want dog", got)
	}
	if change.Notes != nil || change.Favorite != nil {
		t.Fatalf("unchanged notes/favorite reported as changed: notes %v favorite %v", change.Notes, change.Favorite)
	}
	if got := strings.Join(change.ApplyToTags(current.Tags), ","); got != "bird,cat,fish" {
		t.Fatalf("ApplyToTags = %q, want bird,cat,fish", got)
	}
}

func TestPlanMemeEditReplacementDiffsAgainstCurrent(t *testing.T) {
	replacement := []string{"cat", "bird"}
	change, err := PlanMemeEdit(Meme{Tags: []string{"cat", "dog"}}, MemeEdit{ReplaceTags: &replacement})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(change.TagsAdded, ",") != "bird" || strings.Join(change.TagsRemoved, ",") != "dog" {
		t.Fatalf("change = +%v -%v, want +[bird] -[dog]", change.TagsAdded, change.TagsRemoved)
	}
	if _, err := PlanMemeEdit(Meme{}, MemeEdit{ReplaceTags: &replacement, TagsAdded: []string{"x"}}); err == nil {
		t.Fatal("an edit with both a replacement and a delta was accepted")
	}
}

// addOnly is the policy of a user who may add tags but not remove them.
var errNoRemove = errors.New("missing remove-tag permission")

func addOnly(change MemeChange) error {
	if len(change.TagsRemoved) > 0 {
		return errNoRemove
	}
	return nil
}

func createEditTestMeme(t *testing.T, store *PostgresStore, token string, tags []string, notes string) Meme {
	t.Helper()
	meme, err := store.Create(context.Background(), CreateInput{
		File:        strings.NewReader("edit test bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        tags,
		Notes:       notes,
		Actor:       AuditActor{UserID: "u-edit", Username: "edit"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Delete(context.Background(), DeleteInput{ID: meme.ID, Actor: AuditActor{UserID: "u-edit", IsSuperAdmin: true}})
	})
	return meme
}

func auditActions(t *testing.T, store *PostgresStore, memeID string) map[string][]string {
	t.Helper()
	entries, err := store.ListMemeAudit(context.Background(), memeID, 200)
	if err != nil {
		t.Fatalf("ListMemeAudit: %v", err)
	}
	out := map[string][]string{}
	for _, entry := range entries {
		key := entry.Actor.UserID + " " + entry.Action
		out[key] = append(out[key], entry.Description)
	}
	return out
}

// Many people adding different tags to the same meme at once must all keep
// their tag. The old read-modify-write replaced the whole set, so the last
// writer dropped everyone else's.
func TestEditMemeConcurrentTagAdditionsAllSurvive(t *testing.T) {
	store := newRegressionStore(t)
	token := "edit" + randToken(t)
	meme := createEditTestMeme(t, store, token, []string{token + "-a"}, "keep me")

	const writers = 16
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	want := []string{token + "-a"}
	for i := range writers {
		tag := fmt.Sprintf("%s-w%02d", token, i)
		want = append(want, tag)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.EditMeme(context.Background(), fmt.Sprintf("u-%d", i), meme.ID, MemeEdit{
				TagsAdded: []string{tag},
				Actor:     AuditActor{UserID: fmt.Sprintf("u-%d", i)},
				Authorize: addOnly,
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EditMeme: %v", err)
		}
	}

	got, err := store.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	if strings.Join(got.Tags, ",") != strings.Join(want, ",") {
		t.Fatalf("tags after %d concurrent additions = %v, want %v", writers, got.Tags, want)
	}
	if got.Notes != "keep me" {
		t.Fatalf("notes = %q, want them untouched", got.Notes)
	}
	actions := auditActions(t, store, meme.ID)
	for i := range writers {
		user := fmt.Sprintf("u-%d", i)
		if added := actions[user+" tag_added"]; len(added) != 1 {
			t.Fatalf("%s audit tag_added = %v, want exactly its own tag", user, added)
		}
		if removed := actions[user+" tag_removed"]; len(removed) != 0 {
			t.Fatalf("%s audit tag_removed = %v, want none", user, removed)
		}
	}
}

// An add-only user's edit that lands while someone else is adding a tag must
// wait for that edit and then add on top of it: it never removes the other
// tag and is never blamed for removing it.
func TestEditMemeAddOnlyNeverRemovesConcurrentlyAddedTag(t *testing.T) {
	store := newRegressionStore(t)
	token := "edit" + randToken(t)
	a, b, c := token+"-a", token+"-b", token+"-c"
	meme := createEditTestMeme(t, store, token, []string{a}, "")

	// B starts adding c and stops while holding the row lock.
	holding := make(chan struct{})
	release := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		_, err := store.EditMeme(context.Background(), "u-b", meme.ID, MemeEdit{
			TagsAdded: []string{c},
			Actor:     AuditActor{UserID: "u-b"},
			Authorize: func(MemeChange) error {
				close(holding)
				<-release
				return nil
			},
		})
		bDone <- err
	}()
	<-holding

	// A, add-only, adds b. It loaded the meme as {a} before c existed.
	var seen MemeChange
	aDone := make(chan error, 1)
	go func() {
		_, err := store.EditMeme(context.Background(), "u-a", meme.ID, MemeEdit{
			TagsAdded: []string{b},
			Actor:     AuditActor{UserID: "u-a"},
			Authorize: func(change MemeChange) error {
				seen = change
				return addOnly(change)
			},
		})
		aDone <- err
	}()
	select {
	case err := <-aDone:
		close(release)
		t.Fatalf("A's edit finished (err %v) while B held the meme's row lock", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-bDone; err != nil {
		t.Fatalf("B's EditMeme: %v", err)
	}
	if err := <-aDone; err != nil {
		t.Fatalf("A's EditMeme: %v", err)
	}
	if strings.Join(seen.TagsAdded, ",") != b || len(seen.TagsRemoved) != 0 {
		t.Fatalf("A's change = +%v -%v, want +[%s] and no removals", seen.TagsAdded, seen.TagsRemoved, b)
	}

	got, err := store.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join([]string{a, b, c}, ","); strings.Join(got.Tags, ",") != want {
		t.Fatalf("tags = %v, want %s", got.Tags, want)
	}

	// A legacy client sending A's stale full set {a, b} would now remove c.
	// That removal is seen under the lock and refused for an add-only user.
	stale := []string{a, b}
	if _, err := store.EditMeme(context.Background(), "u-a", meme.ID, MemeEdit{
		ReplaceTags: &stale,
		Actor:       AuditActor{UserID: "u-a"},
		Authorize:   addOnly,
	}); !errors.Is(err, errNoRemove) {
		t.Fatalf("stale full replacement by an add-only user: err = %v, want %v", err, errNoRemove)
	}
	got, err = store.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Tags, c) {
		t.Fatalf("tags = %v: the add-only user's refused edit removed %s", got.Tags, c)
	}

	actions := auditActions(t, store, meme.ID)
	if removed := actions["u-a tag_removed"]; len(removed) != 0 {
		t.Fatalf("audit blames the add-only user for removals: %v", removed)
	}
	if added := actions["u-a tag_added"]; len(added) != 1 || !strings.Contains(added[0], b) {
		t.Fatalf("u-a tag_added audit = %v, want just %s", added, b)
	}
	if added := actions["u-b tag_added"]; len(added) != 1 || !strings.Contains(added[0], c) {
		t.Fatalf("u-b tag_added audit = %v, want just %s", added, c)
	}
}

// The legacy full-replacement payload still works and is diffed against the
// meme's current tags, so its audit entries are what it really changed.
func TestEditMemeReplaceTagsAuditsActualChange(t *testing.T) {
	store := newRegressionStore(t)
	token := "edit" + randToken(t)
	a, b, c := token+"-a", token+"-b", token+"-c"
	meme := createEditTestMeme(t, store, token, []string{a, b}, "notes stay")

	replacement := []string{b, c}
	got, err := store.EditMeme(context.Background(), "u-r", meme.ID, MemeEdit{ReplaceTags: &replacement, Actor: AuditActor{UserID: "u-r"}})
	if err != nil {
		t.Fatalf("EditMeme: %v", err)
	}
	if strings.Join(got.Tags, ",") != b+","+c || got.Notes != "notes stay" {
		t.Fatalf("after replacement: tags %v notes %q", got.Tags, got.Notes)
	}
	actions := auditActions(t, store, meme.ID)
	if added := actions["u-r tag_added"]; len(added) != 1 || !strings.Contains(added[0], c) {
		t.Fatalf("tag_added audit = %v, want just %s", added, c)
	}
	if removed := actions["u-r tag_removed"]; len(removed) != 1 || !strings.Contains(removed[0], a) {
		t.Fatalf("tag_removed audit = %v, want just %s", removed, a)
	}
}

// Notes and the favorite are written only when an edit sets them.
func TestEditMemeLeavesUnsetFieldsAlone(t *testing.T) {
	store := newRegressionStore(t)
	token := "edit" + randToken(t)
	meme := createEditTestMeme(t, store, token, []string{token + "-a"}, "original")
	if _, err := store.SetFavorite(context.Background(), "u-f", meme.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := store.EditMeme(context.Background(), "u-f", meme.ID, MemeEdit{TagsAdded: []string{token + "-b"}})
	if err != nil {
		t.Fatalf("EditMeme tags: %v", err)
	}
	if got.Notes != "original" || !got.Favorite {
		t.Fatalf("tags-only edit changed notes %q / favorite %v", got.Notes, got.Favorite)
	}

	notes := "  edited  "
	got, err = store.EditMeme(context.Background(), "u-f", meme.ID, MemeEdit{Notes: &notes})
	if err != nil {
		t.Fatalf("EditMeme notes: %v", err)
	}
	if got.Notes != "edited" || len(got.Tags) != 2 || !got.Favorite {
		t.Fatalf("notes-only edit: notes %q tags %v favorite %v", got.Notes, got.Tags, got.Favorite)
	}

	unfavorite := false
	got, err = store.EditMeme(context.Background(), "u-f", meme.ID, MemeEdit{Favorite: &unfavorite, Actor: AuditActor{UserID: "u-f"}})
	if err != nil {
		t.Fatalf("EditMeme favorite: %v", err)
	}
	if got.Favorite || got.Notes != "edited" || len(got.Tags) != 2 {
		t.Fatalf("favorite-only edit: favorite %v notes %q tags %v", got.Favorite, got.Notes, got.Tags)
	}
	if unfav := auditActions(t, store, meme.ID)["u-f unfavorited"]; len(unfav) != 1 {
		t.Fatalf("unfavorited audit = %v, want one entry", unfav)
	}

	if _, err := store.EditMeme(context.Background(), "", "no-such-meme-"+token, MemeEdit{TagsAdded: []string{"x"}}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("editing a missing meme: err = %v, want os.ErrNotExist", err)
	}
}
