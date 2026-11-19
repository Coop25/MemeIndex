package client

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"memeindex/internal/accessor"
)

// These tests cover #78 in the client package: handlers hand their request
// context to the stores, background jobs hand theirs, and the writes that
// must finish once started are given a context the client cannot cancel.

type requestContextKey struct{}

// requestScoped returns a context carrying a marker, so a fake store can tell
// whether the context it was given came from the caller.
func requestScoped(parent context.Context) context.Context {
	return context.WithValue(parent, requestContextKey{}, "request")
}

func fromRequest(ctx context.Context) bool {
	return ctx.Value(requestContextKey{}) == "request"
}

// contextCheckingUserStore fails every lookup made on a context that did not
// come from the caller, and honours cancellation like the Postgres store.
type contextCheckingUserStore struct {
	*fakeSessionStore

	mu     sync.Mutex
	missed []string
}

func (s *contextCheckingUserStore) check(name string, ctx context.Context) error {
	if !fromRequest(ctx) {
		s.mu.Lock()
		s.missed = append(s.missed, name)
		s.mu.Unlock()
	}
	return ctx.Err()
}

func (s *contextCheckingUserStore) UpsertDiscordProfile(ctx context.Context, user discordUser) error {
	if err := s.check("UpsertDiscordProfile", ctx); err != nil {
		return err
	}
	return s.fakeSessionStore.UpsertDiscordProfile(ctx, user)
}

func (s *contextCheckingUserStore) SessionVersion(ctx context.Context, userID string) (int64, error) {
	if err := s.check("SessionVersion", ctx); err != nil {
		return 0, err
	}
	return s.fakeSessionStore.SessionVersion(ctx, userID)
}

func (s *contextCheckingUserStore) GetUser(ctx context.Context, userID string) (managedUserRecord, bool, error) {
	if err := s.check("GetUser", ctx); err != nil {
		return managedUserRecord{}, false, err
	}
	return s.fakeSessionStore.GetUser(ctx, userID)
}

func TestLoginUserLookupsUseTheRequestContext(t *testing.T) {
	store := &contextCheckingUserStore{fakeSessionStore: &fakeSessionStore{version: 4, perms: authPermissions{CanView: true}}}
	a := newRevocationAuthService(store)

	session, _, err := a.createSession(requestScoped(context.Background()), discordUser{ID: "u-ctx", Username: "ctx"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if !session.Permissions.CanView || session.SessionVersion != 4 {
		t.Fatalf("session = %+v, want the stored permissions and version", session)
	}
	if len(store.missed) > 0 {
		t.Fatalf("user store calls not made on the request context: %v", store.missed)
	}
}

// TestCancelledRetryJobIsNotReportedAsRevoked checks that a link retry
// interrupted by shutdown during its permission check says so, instead of
// recording that the uploader's permission was revoked.
func TestCancelledRetryJobIsNotReportedAsRevoked(t *testing.T) {
	store := &contextCheckingUserStore{fakeSessionStore: &fakeSessionStore{version: 1, perms: authPermissions{CanView: true, CanUpload: true}}}
	s := &Server{auth: newRevocationAuthService(store)}

	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	cancel()
	err := s.processRetriedLinkJob(ctx, LinkRetryJob{Actor: accessor.AuditActor{UserID: "u-ctx"}, SourceURL: "https://youtu.be/test"})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "revoked") {
		t.Fatalf("cancelled retry: err = %v, want context.Canceled", err)
	}
}

// leavesAfterUpsert is a user store whose client goes away right after the
// login's profile upsert has been written.
type leavesAfterUpsert struct {
	*contextCheckingUserStore
	leave context.CancelFunc
}

func (s *leavesAfterUpsert) UpsertDiscordProfile(ctx context.Context, user discordUser) error {
	err := s.contextCheckingUserStore.UpsertDiscordProfile(ctx, user)
	s.leave()
	return err
}

// A login abandoned after its profile upsert must fail as abandoned. Building
// a session from lookups the cancel cut short would hand the callback an
// approved user with no permissions, which it then records as a
// login_awaiting_approval security event.
func TestLoginAbandonedAfterTheUpsertIsNotAnUnapprovedLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	defer cancel()
	store := &leavesAfterUpsert{
		contextCheckingUserStore: &contextCheckingUserStore{fakeSessionStore: &fakeSessionStore{version: 4, perms: authPermissions{CanView: true}}},
		leave:                    cancel,
	}
	a := newRevocationAuthService(store)

	session, _, err := a.createSession(ctx, discordUser{ID: "u-ctx", Username: "ctx"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("createSession after the client left: session = %+v, err = %v; want context.Canceled", session, err)
	}
}
