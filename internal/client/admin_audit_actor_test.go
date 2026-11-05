package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// auditFeedStore serves a fixed audit feed and honors the actor filter, so the
// test proves the handler passes ?actor= through and returns filtered paging.
type auditFeedStore struct {
	*accessor.MemeStore
	accessor.AuditLogStore
	events    []accessor.GlobalMemeAuditEntry
	gotActors []string
}

func (a *auditFeedStore) ListAuditFeed(offset, limit int, actorUserID string) (accessor.PagedAuditFeed, error) {
	a.gotActors = append(a.gotActors, actorUserID)
	out := []accessor.GlobalMemeAuditEntry{}
	for _, e := range a.events {
		if actorUserID == "" || e.Actor.UserID == actorUserID {
			out = append(out, e)
		}
	}
	return accessor.PagedAuditFeed{Events: out, Total: len(out), NextOffset: len(out)}, nil
}

func TestAuditLogsFilterByActor(t *testing.T) {
	s, store, _ := newShareTestServer(t)
	s.config.DataDir = t.TempDir()
	mk := func(id int64, user string) accessor.GlobalMemeAuditEntry {
		e := accessor.GlobalMemeAuditEntry{}
		e.ID, e.Action, e.Actor.UserID = id, "upload", user
		return e
	}
	feed := &auditFeedStore{MemeStore: store, events: []accessor.GlobalMemeAuditEntry{mk(1, "a"), mk(2, "b"), mk(3, "a")}}
	s.managers = manager.NewMemeManager(feed)
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true, CanManageUsers: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", SessionVersion: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	get := func(query string) accessor.PagedAuditFeed {
		r := httptest.NewRequest(http.MethodGet, "https://example.com/api/admin/audit-logs"+query, nil)
		w := httptest.NewRecorder()
		s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
		r.AddCookie(w.Result().Cookies()[0])
		w = httptest.NewRecorder()
		s.Routes().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var out accessor.PagedAuditFeed
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := get("?actor=a")
	if got.Total != 2 || len(got.Events) != 2 {
		t.Fatalf("actor=a total %d events %d, want 2/2", got.Total, len(got.Events))
	}
	for _, e := range got.Events {
		if e.Actor.UserID != "a" {
			t.Errorf("event %d actor %q, want a", e.ID, e.Actor.UserID)
		}
	}
	if all := get(""); all.Total != 3 {
		t.Errorf("unfiltered total = %d, want 3", all.Total)
	}
}
