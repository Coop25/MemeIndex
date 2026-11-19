package client

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The database pool cancels a query on the server when its request goes
// away, and Postgres reports that as query_canceled (57014), not as
// context.Canceled. requestAbandoned has to recognise both, or every
// abandoned browse is logged and answered as a 500. The same SQLSTATE also
// comes from statement_timeout, which on a live request is a real failure.
func TestRequestAbandonedRecognisesServerSideCancel(t *testing.T) {
	serverCancel := &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"}
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	gone := httptest.NewRequest("GET", "/api/memes", nil).WithContext(cancelled)
	live := httptest.NewRequest("GET", "/api/memes", nil)

	cases := []struct {
		name string
		live bool
		err  error
		want bool
	}{
		{"client cancel, pgx context error", false, fmt.Errorf("list memes: %w", context.Canceled), true},
		{"client cancel, server query_canceled", false, fmt.Errorf("list memes: %w", serverCancel), true},
		{"statement timeout on a live request", true, fmt.Errorf("list memes: %w", timeout), false},
		{"other error after the client left", false, fmt.Errorf("list memes: %w", &pgconn.PgError{Code: "42P01"}), false},
	}
	for _, tc := range cases {
		r := gone
		if tc.live {
			r = live
		}
		if got := requestAbandoned(r, tc.err); got != tc.want {
			t.Errorf("%s: requestAbandoned = %v, want %v", tc.name, got, tc.want)
		}
	}
}
