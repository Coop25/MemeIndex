package client

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestMetricSetExposition(t *testing.T) {
	m := newMetricSet()
	m.setBuildVersion("v9.9.9")
	m.setGauge("memeindex_search_trgm_index_available", 1)

	// 3 fast GETs, one slow GET, one POST 500.
	m.observe(http.MethodGet, 200, 0.003)
	m.observe(http.MethodGet, 200, 0.004)
	m.observe(http.MethodGet, 404, 0.006)
	m.observe(http.MethodGet, 200, 3.0)
	m.observe(http.MethodPost, 500, 0.2)
	m.addInFlight(2)

	var sb strings.Builder
	m.writeExposition(&sb)
	out := sb.String()

	wantContains := []string{
		`memeindex_http_requests_total{method="GET",code="200"} 3`,
		`memeindex_http_requests_total{method="GET",code="404"} 1`,
		`memeindex_http_requests_total{method="POST",code="500"} 1`,
		`memeindex_http_request_duration_seconds_bucket{method="GET",le="0.005"} 2`,
		`memeindex_http_request_duration_seconds_bucket{method="GET",le="+Inf"} 4`,
		`memeindex_http_request_duration_seconds_count{method="GET"} 4`,
		`memeindex_http_requests_in_flight 2`,
		`memeindex_build_info{version="v9.9.9"} 1`,
		`memeindex_search_trgm_index_available 1`,
		"# TYPE memeindex_http_request_duration_seconds histogram",
	}
	for _, want := range wantContains {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\n--- full output ---\n%s", want, out)
		}
	}

	// Cumulative buckets: le="0.005" <= le="0.01" <= +Inf.
	if got := bucketValue(t, out, "GET", "0.005"); got != 2 {
		t.Errorf("GET le=0.005 bucket = %d, want 2", got)
	}
	if got := bucketValue(t, out, "GET", "0.01"); got != 3 {
		t.Errorf("GET le=0.01 bucket = %d, want 3", got)
	}
}

func TestMetricsMiddlewareRecordsStatusAndSkipsProbes(t *testing.T) {
	original := metrics
	metrics = newMetricSet()
	t.Cleanup(func() { metrics = original })

	h := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/memes", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := metrics.requestsTotal[requestKey{method: http.MethodGet, code: http.StatusTeapot}]; got != 1 {
		t.Fatalf("GET 418 recorded %d times, want 1 (map: %v)", got, metrics.requestsTotal)
	}
	if hist := metrics.latency[http.MethodGet]; hist == nil || hist.count != 1 {
		t.Fatalf("health probe should not be counted; latency = %+v", hist)
	}
}

func TestMetricsHandlerRejectsNonGet(t *testing.T) {
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /metrics = %d, want 405", rec.Code)
	}
}

func bucketValue(t *testing.T, exposition, method, le string) int {
	t.Helper()
	prefix := `memeindex_http_request_duration_seconds_bucket{method="` + method + `",le="` + le + `"} `
	for _, line := range strings.Split(exposition, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			return n
		}
	}
	t.Fatalf("bucket line not found for method=%s le=%s", method, le)
	return 0
}
