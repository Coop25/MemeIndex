package accessor

import (
	"context"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

func TestListAuditFeedFilterByActor(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	store, err := NewPostgresStore(context.Background(), dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)

	token := "auditactor" + randToken(t)
	userA, userB := token+"-a", token+"-b"
	for i, u := range []string{userA, userB, userA} {
		if _, err := store.Create(CreateInput{
			File:        strings.NewReader("bytes " + u + string(rune('a'+i))),
			Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
			Filename:    u + ".png",
			ContentType: "image/png",
			Actor:       AuditActor{UserID: u, Username: u},
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	feed, err := store.ListAuditFeed(0, 50, userA)
	if err != nil {
		t.Fatalf("ListAuditFeed: %v", err)
	}
	if feed.Total != 2 || len(feed.Events) != 2 {
		t.Fatalf("actor A total %d events %d, want 2/2", feed.Total, len(feed.Events))
	}
	for _, e := range feed.Events {
		if e.Actor.UserID != userA {
			t.Errorf("event actor %q, want %q", e.Actor.UserID, userA)
		}
	}
	paged, err := store.ListAuditFeed(0, 1, userA)
	if err != nil || paged.Total != 2 || len(paged.Events) != 1 || !paged.HasMore {
		t.Fatalf("paged = %+v err %v, want total 2, 1 event, has_more", paged, err)
	}
	all, err := store.ListAuditFeed(0, 1, "")
	if err != nil || all.Total < 3 {
		t.Fatalf("unfiltered total = %d err %v, want >= 3", all.Total, err)
	}
}
