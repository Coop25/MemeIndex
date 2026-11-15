package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// failingQueryStore is a querying store whose SQL reads all fail, as they
// would during a database outage.
type failingQueryStore struct {
	accessor.Store
}

var errQueryDown = errors.New("database unavailable")

func (failingQueryStore) QueryMemes(context.Context, accessor.MemeQuery) (accessor.MemeQueryPage, error) {
	return accessor.MemeQueryPage{}, errQueryDown
}

func (failingQueryStore) MemeDashboard(context.Context, string) (accessor.MemeDashboardData, error) {
	return accessor.MemeDashboardData{}, errQueryDown
}

func (failingQueryStore) TagCounts(context.Context, int) ([]accessor.TagCount, error) {
	return nil, errQueryDown
}

// The browse, dashboard and popular-tag endpoints answer a failed SQL read
// with a 500 instead of an empty (or full-scan) 200 that would look like the
// library had changed.
func TestMemeReadEndpointsReturn500WhenQueryFails(t *testing.T) {
	base, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	s := &Server{managers: manager.NewMemeManager(failingQueryStore{Store: base})}

	cases := []struct {
		name    string
		target  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"list", "/api/memes", s.listMemes},
		{"dashboard", "/api/dashboard", s.handleDashboard},
		{"newest probe", "/api/dashboard?probe=newest", s.handleDashboard},
		{"popular tags", "/api/tags/popular", s.handlePopularTags},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 (body %q)", rec.Code, rec.Body.String())
			}
		})
	}
}

// cancelledQueryStore fails the way PostgresStore does once the request's
// context is cancelled.
type cancelledQueryStore struct {
	accessor.Store
}

func (cancelledQueryStore) QueryMemes(ctx context.Context, _ accessor.MemeQuery) (accessor.MemeQueryPage, error) {
	return accessor.MemeQueryPage{}, ctx.Err()
}

func (cancelledQueryStore) MemeDashboard(ctx context.Context, _ string) (accessor.MemeDashboardData, error) {
	return accessor.MemeDashboardData{}, ctx.Err()
}

func (cancelledQueryStore) TagCounts(ctx context.Context, _ int) ([]accessor.TagCount, error) {
	return nil, ctx.Err()
}

// A viewer who navigates away cancels the query; that is not a server error,
// so nothing is written back (in particular, no 500).
func TestMemeReadEndpointsStaySilentWhenRequestIsCancelled(t *testing.T) {
	base, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	s := &Server{managers: manager.NewMemeManager(cancelledQueryStore{Store: base})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		target  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/api/memes", s.listMemes},
		{"/api/dashboard", s.handleDashboard},
		{"/api/dashboard?probe=newest", s.handleDashboard},
		{"/api/tags/popular", s.handlePopularTags},
	} {
		rec := httptest.NewRecorder()
		tc.handler(rec, httptest.NewRequest(http.MethodGet, tc.target, nil).WithContext(ctx))
		if rec.Code == http.StatusInternalServerError || rec.Body.Len() != 0 {
			t.Errorf("%s: status %d body %q, want nothing written", tc.target, rec.Code, rec.Body.String())
		}
	}
}
