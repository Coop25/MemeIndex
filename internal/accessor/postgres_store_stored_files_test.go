package accessor

import (
	"context"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

// AllStoredFiles feeds the storage orphan scan: a hidden or pending-delete row
// left out would make its file look orphaned and deletable.
func TestPostgresAllStoredFilesIncludesHiddenAndPendingDelete(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres stored files test")
	}
	ctx := context.Background()
	store, err := NewPostgresStore(ctx, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)
	token := "sf" + randToken(t)
	admin := AuditActor{UserID: "u-sf", Username: "sf", IsSuperAdmin: true}
	create := func(name string) Meme {
		t.Helper()
		m, err := store.Create(CreateInput{
			File:        strings.NewReader("fake png bytes " + name),
			Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
			Filename:    token + "-" + name + ".png",
			ContentType: "image/png",
			Actor:       admin,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		t.Cleanup(func() { store.Delete(DeleteInput{ID: m.ID, Actor: admin}) })
		return m
	}
	visible := create("visible")
	hidden := create("hidden")
	pending := create("pending")
	if _, err := store.pool.Exec(ctx, `UPDATE memes SET hidden_from_app = TRUE WHERE id = $1`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	// A non-admin delete only flags the row for approval (pending_delete).
	res, err := store.Delete(DeleteInput{ID: pending.ID, Actor: AuditActor{UserID: "u-viewer", Username: "viewer"}})
	if err != nil || !res.PendingApproval {
		t.Fatalf("request delete = %+v, %v", res, err)
	}

	files, err := store.AllStoredFiles()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]StoredFile{}
	for _, f := range files {
		byID[f.ID] = f
	}
	for _, want := range []struct {
		meme   Meme
		hidden bool
	}{{visible, false}, {hidden, true}, {pending, true}} {
		got, ok := byID[want.meme.ID]
		if !ok {
			t.Errorf("%s missing from AllStoredFiles", want.meme.OriginalName)
			continue
		}
		if got.StoredName != want.meme.StoredName || got.Hidden != want.hidden || got.SizeBytes != want.meme.SizeBytes {
			t.Errorf("%s = %+v, want stored %s hidden=%v size=%d", want.meme.OriginalName, got, want.meme.StoredName, want.hidden, want.meme.SizeBytes)
		}
	}
}
