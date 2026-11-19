package client

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// newPostgresPatchTestServer is newPatchTestServer on a real PostgresStore,
// whose row lock is what the concurrent PATCH tests rely on.
func newPostgresPatchTestServer(t *testing.T, tags []string) (*Server, *accessor.PostgresStore, accessor.Meme, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	store, err := accessor.NewPostgresStore(context.Background(), dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)

	token := "patch" + randomHex(t)
	tagged := make([]string, len(tags))
	for i, tag := range tags {
		tagged[i] = token + "-" + tag
	}
	meme, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("patch test bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        tagged,
		Notes:       "original notes",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Delete(accessor.DeleteInput{ID: meme.ID, Actor: accessor.AuditActor{IsSuperAdmin: true}})
	})
	return &Server{managers: manager.NewMemeManager(store), auth: &authService{}}, store, meme, token
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", b)
}

func patchMemeAs(server *Server, userID string, permissions authPermissions, id, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPatch, "/api/memes/"+id, strings.NewReader(body))
	req = req.WithContext(contextWithSession(req.Context(), authSession{UserID: userID, Permissions: permissions}))
	rec := httptest.NewRecorder()
	http.HandlerFunc(server.handleMemeByID).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

var (
	addOnlyPermissions    = authPermissions{CanView: true, CanAddTags: true}
	removeOnlyPermissions = authPermissions{CanView: true, CanRemoveTags: true}
	fullTagPermissions    = authPermissions{CanView: true, CanAddTags: true, CanRemoveTags: true}
)

// Add-only users adding tags at the same moment as a full tagger must all keep
// their tags, and none of them may be recorded as removing anything.
func TestConcurrentTagPatchesKeepEveryTag(t *testing.T) {
	server, store, meme, token := newPostgresPatchTestServer(t, []string{"a"})

	type patch struct {
		user        string
		permissions authPermissions
		tag         string
	}
	patches := []patch{{user: "full", permissions: fullTagPermissions, tag: token + "-c"}}
	for i := range 8 {
		patches = append(patches, patch{user: fmt.Sprintf("adder-%d", i), permissions: addOnlyPermissions, tag: fmt.Sprintf("%s-add%d", token, i)})
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	failures := make(chan string, len(patches))
	for _, p := range patches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"tags_added":[%q]}`, p.tag)
			if code, resp := patchMemeAs(server, p.user, p.permissions, meme.ID, body); code != http.StatusOK {
				failures <- fmt.Sprintf("%s: status %d: %s", p.user, code, resp)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}

	got, err := store.GetByID("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{token + "-a"}
	for _, p := range patches {
		want = append(want, p.tag)
	}
	slices.Sort(want)
	if strings.Join(got.Tags, ",") != strings.Join(want, ",") {
		t.Fatalf("tags = %v, want %v", got.Tags, want)
	}
	if got.Notes != "original notes" {
		t.Fatalf("notes = %q, want them untouched by tag-only patches", got.Notes)
	}
	entries, err := store.ListMemeAudit(meme.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Action == "tag_removed" {
			t.Fatalf("audit records a removal no one made: %+v", entry)
		}
	}
}

// The issue's scenario through the legacy full-replacement payload: an
// add-only user whose client still shows {a} sends {a, b} after someone else
// added c. That would remove c, so it is refused and c stays. Likewise a
// remove-only user's stale set cannot add a tag back.
func TestStaleFullTagPatchCannotExceedPermissions(t *testing.T) {
	server, store, meme, token := newPostgresPatchTestServer(t, []string{"a"})
	a, b, c := token+"-a", token+"-b", token+"-c"

	if code, body := patchMemeAs(server, "full", fullTagPermissions, meme.ID, fmt.Sprintf(`{"tags_added":[%q]}`, c)); code != http.StatusOK {
		t.Fatalf("adding c: status %d: %s", code, body)
	}
	code, body := patchMemeAs(server, "adder", addOnlyPermissions, meme.ID, fmt.Sprintf(`{"tags":[%q,%q]}`, a, b))
	if code != http.StatusForbidden || !strings.Contains(body, "missing remove-tag permission") {
		t.Fatalf("stale full set from an add-only user: status %d body %s, want 403 missing remove-tag permission", code, body)
	}
	// The same user's delta adds b without touching c.
	if code, body := patchMemeAs(server, "adder", addOnlyPermissions, meme.ID, fmt.Sprintf(`{"tags_added":[%q]}`, b)); code != http.StatusOK {
		t.Fatalf("add-only delta: status %d: %s", code, body)
	}
	got, err := store.GetByID("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join([]string{a, b, c}, ","); strings.Join(got.Tags, ",") != want {
		t.Fatalf("tags = %v, want %s", got.Tags, want)
	}

	// A remove-only user removes a; a stale full set that still has a would
	// add it back and is refused.
	if code, body := patchMemeAs(server, "remover", removeOnlyPermissions, meme.ID, fmt.Sprintf(`{"tags_removed":[%q]}`, a)); code != http.StatusOK {
		t.Fatalf("remove-only delta: status %d: %s", code, body)
	}
	code, body = patchMemeAs(server, "remover", removeOnlyPermissions, meme.ID, fmt.Sprintf(`{"tags":[%q,%q]}`, a, b))
	if code != http.StatusForbidden || !strings.Contains(body, "missing add-tag permission") {
		t.Fatalf("stale full set from a remove-only user: status %d body %s, want 403 missing add-tag permission", code, body)
	}
	got, err = store.GetByID("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join([]string{b, c}, ","); strings.Join(got.Tags, ",") != want {
		t.Fatalf("tags = %v, want %s", got.Tags, want)
	}

	entries, err := store.ListMemeAudit(meme.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Actor.UserID == "adder" && entry.Action != "tag_added" {
			t.Fatalf("add-only user has a %q audit entry: %+v", entry.Action, entry)
		}
		if entry.Actor.UserID == "remover" && entry.Action != "tag_removed" {
			t.Fatalf("remove-only user has a %q audit entry: %+v", entry.Action, entry)
		}
	}
}
