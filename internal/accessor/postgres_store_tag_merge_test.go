package accessor

import (
	"context"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"testing"

	"memeindex/internal/dbschema"
)

type tagMergeFixture struct {
	t     *testing.T
	store *PostgresStore
	token string
}

func newTagMergeFixture(t *testing.T) *tagMergeFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres tag merge tests")
	}
	ctx := context.Background()
	store, err := NewPostgresStore(ctx, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)
	if err := dbschema.Apply(ctx, store.pool, "015_audiobooks.sql", "026_audiobook_tags.sql", "027_app_user_audiobook_tag_restrictions.sql"); err != nil {
		t.Fatalf("apply audiobook schema: %v", err)
	}
	return &tagMergeFixture{t: t, store: store, token: "tm" + randToken(t)}
}

func (f *tagMergeFixture) tag(suffix string) string { return f.token + "-" + suffix }

func (f *tagMergeFixture) tagID(name string) int64 {
	f.t.Helper()
	var id int64
	if err := f.store.pool.QueryRow(context.Background(), `INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, name).Scan(&id); err != nil {
		f.t.Fatalf("tag id: %v", err)
	}
	return id
}

func (f *tagMergeFixture) audiobook(tags ...string) string {
	f.t.Helper()
	ctx := context.Background()
	id := "ab-" + randToken(f.t)
	if _, err := f.store.pool.Exec(ctx, `INSERT INTO audiobooks (id, title, filename, size_bytes, storage_path) VALUES ($1, 'T', 'f.mp3', 1, '/x')`, id); err != nil {
		f.t.Fatalf("insert audiobook: %v", err)
	}
	f.t.Cleanup(func() { f.store.pool.Exec(ctx, `DELETE FROM audiobooks WHERE id = $1`, id) })
	for _, tag := range tags {
		if _, err := f.store.pool.Exec(ctx, `INSERT INTO audiobook_tags (audiobook_id, tag_id) VALUES ($1, $2)`, id, f.tagID(tag)); err != nil {
			f.t.Fatalf("tag audiobook: %v", err)
		}
	}
	return id
}

func (f *tagMergeFixture) meme(tags ...string) string {
	f.t.Helper()
	actor := AuditActor{UserID: "u-tm", Username: "tm", IsSuperAdmin: true}
	m, err := f.store.Create(CreateInput{
		File:        strings.NewReader("fake png bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    f.token + "-pic.png",
		ContentType: "image/png",
		Tags:        tags,
		Actor:       actor,
	})
	if err != nil {
		f.t.Fatalf("Create meme: %v", err)
	}
	f.t.Cleanup(func() { f.store.Delete(DeleteInput{ID: m.ID, Actor: actor}) })
	return m.ID
}

func (f *tagMergeFixture) restrict(user string, tags ...string) {
	f.t.Helper()
	ctx := context.Background()
	f.t.Cleanup(func() {
		f.store.pool.Exec(ctx, `DELETE FROM app_user_audiobook_tag_restrictions WHERE user_id = $1`, user)
	})
	for _, tag := range tags {
		if _, err := f.store.pool.Exec(ctx, `INSERT INTO app_user_audiobook_tag_restrictions (user_id, tag_id) VALUES ($1, $2)`, user, f.tagID(tag)); err != nil {
			f.t.Fatalf("restrict: %v", err)
		}
	}
}

func (f *tagMergeFixture) audiobookTags(id string) []string {
	f.t.Helper()
	return f.names(`SELECT t.name FROM audiobook_tags at JOIN tags t ON t.id = at.tag_id WHERE at.audiobook_id = $1 ORDER BY t.name`, id)
}

func (f *tagMergeFixture) restrictions(user string) []string {
	f.t.Helper()
	return f.names(`SELECT t.name FROM app_user_audiobook_tag_restrictions r JOIN tags t ON t.id = r.tag_id WHERE r.user_id = $1 ORDER BY t.name`, user)
}

func (f *tagMergeFixture) names(sql string, arg string) []string {
	f.t.Helper()
	rows, err := f.store.pool.Query(context.Background(), sql, arg)
	if err != nil {
		f.t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func (f *tagMergeFixture) tagExists(name string) bool {
	f.t.Helper()
	var n int
	if err := f.store.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tags WHERE name = $1`, name).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n > 0
}

func (f *tagMergeFixture) cleanupTags(names ...string) {
	f.t.Cleanup(func() {
		for _, n := range names {
			f.store.pool.Exec(context.Background(), `DELETE FROM tags WHERE name = $1`, n)
		}
	})
}

var mergeActor = AuditActor{UserID: "admin", Username: "admin", IsSuperAdmin: true}

func TestMergeTagMovesAudiobookTags(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	f.meme(src)
	b := f.audiobook(src)

	counts, err := f.store.MergeTag(src, dst, mergeActor)
	if err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if want := (TagMergeCounts{Memes: 1, Audiobooks: 1, Restrictions: 0}); counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	if got := f.audiobookTags(b); !slices.Equal(got, []string{dst}) {
		t.Fatalf("audiobook tags = %v, want [%s]", got, dst)
	}
	if f.tagExists(src) {
		t.Fatalf("source tag %q still exists", src)
	}
}

func TestMergeTagAudiobookOnlySource(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	b := f.audiobook(src)

	counts, err := f.store.MergeTag(src, dst, mergeActor)
	if err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if counts.Audiobooks != 1 || counts.Memes != 0 {
		t.Fatalf("counts = %+v, want Audiobooks 1, Memes 0", counts)
	}
	if got := f.audiobookTags(b); !slices.Equal(got, []string{dst}) {
		t.Fatalf("audiobook tags = %v, want [%s]", got, dst)
	}
	if f.tagExists(src) {
		t.Fatalf("source tag %q still exists", src)
	}
}

func TestMergeTagTargetAlreadyOnAudiobook(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	b := f.audiobook(src, dst)

	counts, err := f.store.MergeTag(src, dst, mergeActor)
	if err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if counts.Audiobooks != 1 {
		t.Fatalf("Audiobooks = %d, want 1 (deduped key still counts)", counts.Audiobooks)
	}
	if got := f.audiobookTags(b); !slices.Equal(got, []string{dst}) {
		t.Fatalf("audiobook tags = %v, want exactly [%s]", got, dst)
	}
}

func TestMergeTagKeepsUserRestricted(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	user := "u-" + f.token
	f.restrict(user, src)

	counts, err := f.store.MergeTag(src, dst, mergeActor)
	if err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if counts.Restrictions != 1 {
		t.Fatalf("Restrictions = %d, want 1", counts.Restrictions)
	}
	if got := f.restrictions(user); !slices.Equal(got, []string{dst}) {
		t.Fatalf("restrictions = %v, want [%s] (empty would mean unrestricted)", got, dst)
	}
}

func TestMergeTagRestrictionTargetAlreadyPresent(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	user := "u-" + f.token
	f.restrict(user, src, dst)

	if _, err := f.store.MergeTag(src, dst, mergeActor); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if got := f.restrictions(user); !slices.Equal(got, []string{dst}) {
		t.Fatalf("restrictions = %v, want [%s]", got, dst)
	}
}
