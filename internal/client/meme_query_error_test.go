package client

import (
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

func (failingQueryStore) QueryMemes(accessor.MemeQuery) (accessor.MemeQueryPage, error) {
	return accessor.MemeQueryPage{}, errQueryDown
}

func (failingQueryStore) MemeDashboard(string) (accessor.MemeDashboardData, error) {
	return accessor.MemeDashboardData{}, errQueryDown
}

func (failingQueryStore) TagCounts(int) ([]accessor.TagCount, error) {
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
