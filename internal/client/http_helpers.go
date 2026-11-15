package client

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// jsonResponseBufferPool recycles the scratch buffers writeJSON marshals into.
// Serializing into a buffer first (instead of streaming json.Encoder straight
// down the socket) lets every JSON response carry a Content-Length and go out in
// a single Write rather than a chunked dribble of ~4 KB flushes.
var jsonResponseBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// maxPooledJSONBuffer bounds the capacity of a buffer kept for reuse so a single
// large response (a long audit feed, a backup manifest) cannot pin megabytes in
// the pool for the life of the process.
const maxPooledJSONBuffer = 1 << 20

func writeJSON(w http.ResponseWriter, status int, payload any) {
	buf := jsonResponseBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= maxPooledJSONBuffer {
			jsonResponseBufferPool.Put(buf)
		}
	}()

	// json.Encoder appends a trailing newline; harmless and matches the previous
	// behaviour. Nothing has been written to w yet, so an encode failure can still
	// surface as a clean error status.
	if err := json.NewEncoder(buf).Encode(payload); err != nil {
		log.Printf("write JSON failed: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write JSON failed: %v", err)
	}
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
