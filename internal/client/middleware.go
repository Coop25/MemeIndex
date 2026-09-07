package client

import (
	"log"
	"net/http"
	"strings"
	"time"
)

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// User-specific API data and login redirects must not survive access revocation.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			setProtectedAssetHeaders(w)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Body != nil &&
			!(r.URL.Path == "/api/memes" && r.Method == http.MethodPost) &&
			r.URL.Path != "/api/admin/backup/import" {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}
