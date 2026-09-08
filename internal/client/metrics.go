package client

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// This file is a small, zero-dependency Prometheus text-format metrics layer.
// It deliberately tracks only a handful of series - HTTP request counts, a
// latency histogram, an in-flight gauge, and a few build/health gauges - and is
// exposed only on the optional debug listener (MEMEINDEX_DEBUG_ADDR), so it adds
// nothing to the public HTTP surface.

// latencyBuckets are the upper bounds (seconds) for the request-duration
// histogram. They match Prometheus' common web-latency defaults.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var metrics = newMetricSet()

type metricSet struct {
	mu sync.Mutex

	requestsTotal map[requestKey]uint64
	latency       map[string]*histogram // keyed by HTTP method

	inFlight int64

	buildVersion string
	gauges       map[string]float64
}

type requestKey struct {
	method string
	code   int
}

type histogram struct {
	// counts[i] is the cumulative number of observations <= latencyBuckets[i];
	// observe() bumps every bucket whose bound the sample fits under, so the
	// slice is already in Prometheus' cumulative "le" form at exposition time.
	counts []uint64
	sum    float64
	count  uint64
}

func newMetricSet() *metricSet {
	return &metricSet{
		requestsTotal: make(map[requestKey]uint64),
		latency:       make(map[string]*histogram),
		gauges:        make(map[string]float64),
	}
}

// setBuildVersion records the running build for the memeindex_build_info series.
func (m *metricSet) setBuildVersion(version string) {
	m.mu.Lock()
	m.buildVersion = version
	m.mu.Unlock()
}

// setGauge publishes an arbitrary named gauge (e.g. search-index availability).
func (m *metricSet) setGauge(name string, value float64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

func (m *metricSet) addInFlight(delta int64) {
	m.mu.Lock()
	m.inFlight += delta
	m.mu.Unlock()
}

func (m *metricSet) observe(method string, code int, seconds float64) {
	method = normalizeMethod(method)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestsTotal[requestKey{method: method, code: code}]++

	h := m.latency[method]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(latencyBuckets))}
		m.latency[method] = h
	}
	h.sum += seconds
	h.count++
	for i, bound := range latencyBuckets {
		if seconds <= bound {
			h.counts[i]++
		}
	}
}

// normalizeMethod bounds label cardinality: anything outside the standard set
// collapses to "other".
func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "other"
	}
}

func (m *metricSet) writeExposition(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	bw := &lineWriter{w: w}

	bw.line("# HELP memeindex_http_requests_total Total HTTP requests handled, by method and response code.")
	bw.line("# TYPE memeindex_http_requests_total counter")
	keys := make([]requestKey, 0, len(m.requestsTotal))
	for k := range m.requestsTotal {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].code < keys[j].code
	})
	for _, k := range keys {
		bw.linef(`memeindex_http_requests_total{method=%q,code="%d"} %d`, k.method, k.code, m.requestsTotal[k])
	}

	bw.line("# HELP memeindex_http_request_duration_seconds Request handling latency in seconds, by method.")
	bw.line("# TYPE memeindex_http_request_duration_seconds histogram")
	methods := make([]string, 0, len(m.latency))
	for method := range m.latency {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	for _, method := range methods {
		h := m.latency[method]
		for i, bound := range latencyBuckets {
			bw.linef(`memeindex_http_request_duration_seconds_bucket{method=%q,le=%q} %d`,
				method, strconv.FormatFloat(bound, 'g', -1, 64), h.counts[i])
		}
		bw.linef(`memeindex_http_request_duration_seconds_bucket{method=%q,le="+Inf"} %d`, method, h.count)
		bw.linef(`memeindex_http_request_duration_seconds_sum{method=%q} %s`,
			method, strconv.FormatFloat(h.sum, 'g', -1, 64))
		bw.linef(`memeindex_http_request_duration_seconds_count{method=%q} %d`, method, h.count)
	}

	bw.line("# HELP memeindex_http_requests_in_flight HTTP requests currently being served.")
	bw.line("# TYPE memeindex_http_requests_in_flight gauge")
	bw.linef("memeindex_http_requests_in_flight %d", m.inFlight)

	if m.buildVersion != "" {
		bw.line("# HELP memeindex_build_info Build metadata; value is always 1.")
		bw.line("# TYPE memeindex_build_info gauge")
		bw.linef("memeindex_build_info{version=%q} 1", m.buildVersion)
	}

	if len(m.gauges) > 0 {
		names := make([]string, 0, len(m.gauges))
		for name := range m.gauges {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			bw.linef("# TYPE %s gauge", name)
			bw.linef("%s %s", name, strconv.FormatFloat(m.gauges[name], 'g', -1, 64))
		}
	}
}

type lineWriter struct {
	w   io.Writer
	err error
}

func (l *lineWriter) line(s string) {
	if l.err != nil {
		return
	}
	_, l.err = io.WriteString(l.w, s+"\n")
}

func (l *lineWriter) linef(format string, args ...any) {
	if l.err != nil {
		return
	}
	l.line(fmt.Sprintf(format, args...))
}

// MetricsMiddleware records request counts and latency for every non-probe
// request. It is cheap (one map lookup under a short-held mutex) and safe to
// wrap the whole handler chain with.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		metrics.addInFlight(1)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			metrics.addInFlight(-1)
			metrics.observe(r.Method, rec.status, time.Since(start).Seconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

// MetricsHandler serves the Prometheus text exposition. Mount it on the debug
// listener only.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		metrics.writeExposition(w)
	})
}

// SetMetricsBuildVersion is called once at startup so the exposition can report
// memeindex_build_info.
func SetMetricsBuildVersion(version string) { metrics.setBuildVersion(version) }

// boolGauge maps a boolean health signal onto the 1/0 convention Prometheus
// gauges use.
func boolGauge(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

// statusRecorder captures the response status code for metrics without otherwise
// altering the response. Unwrap keeps http.ResponseController (flush, deadlines,
// hijack) working through it.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
