package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestWriteJSONSetsContentLengthAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]any{"hello": "world", "n": 3, "tags": []string{"a", "b"}}

	writeJSON(rec, http.StatusCreated, payload)

	resp := rec.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	body := rec.Body.Bytes()
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		t.Fatal("Content-Length was not set")
	}
	if n, err := strconv.Atoi(cl); err != nil || n != len(body) {
		t.Fatalf("Content-Length = %q, body is %d bytes", cl, len(body))
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if decoded["hello"] != "world" {
		t.Fatalf("round-tripped payload = %#v", decoded)
	}
}

// Exercises the pooled buffer across many calls, including one large enough to
// trip the max-pooled-capacity guard, to make sure nothing is retained or
// corrupted between responses.
func TestWriteJSONReusesBufferSafely(t *testing.T) {
	big := make([]string, 5000)
	for i := range big {
		big[i] = "tag-value-" + strconv.Itoa(i)
	}

	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		payload := map[string]any{"i": i}
		if i%10 == 0 {
			payload["big"] = big
		}
		writeJSON(rec, http.StatusOK, payload)

		var decoded map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("iteration %d: invalid JSON: %v", i, err)
		}
		if got := int(decoded["i"].(float64)); got != i {
			t.Fatalf("iteration %d: got i=%d", i, got)
		}
		if _, hasBig := decoded["big"]; hasBig != (i%10 == 0) {
			t.Fatalf("iteration %d: stale buffer contents leaked (big present=%v)", i, hasBig)
		}
	}
}
