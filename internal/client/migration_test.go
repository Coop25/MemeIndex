package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationTokenIssueValidateRevoke(t *testing.T) {
	var store migrationTokenStore
	if store.valid("") || store.status().Active {
		t.Fatal("a fresh store must have no active token")
	}
	first, status, err := store.issue()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "mig_") || !status.Active || status.ExpiresAt.Sub(*status.CreatedAt) != migrationTokenTTL {
		t.Fatalf("unexpected token %q / status %+v", first, status)
	}
	if !store.valid(first) || store.valid(first+"x") {
		t.Fatal("only the exact issued token may validate")
	}
	second, _, _ := store.issue()
	if store.valid(first) || !store.valid(second) {
		t.Fatal("issuing a new token must replace the old one")
	}
	store.mu.Lock()
	store.expiresAt = time.Now().Add(-time.Second)
	store.mu.Unlock()
	if store.valid(second) || store.status().Active {
		t.Fatal("an expired token must not validate")
	}
	third, _, _ := store.issue()
	store.revoke()
	if store.valid(third) {
		t.Fatal("a revoked token must not validate")
	}
}

func TestNormalizeMigrationSource(t *testing.T) {
	for input, want := range map[string]string{
		"192.168.1.20":                "http://192.168.1.20:8080",
		" 192.168.1.20:9000 ":         "http://192.168.1.20:9000",
		"nas.local":                   "http://nas.local:8080",
		"[fd00::5]":                   "http://[fd00::5]:8080",
		"https://memes.example.com/":  "https://memes.example.com",
		"http://10.0.0.2/memeindex/":  "http://10.0.0.2/memeindex",
		"http://10.0.0.2:8080?x=1#yz": "http://10.0.0.2:8080",
	} {
		got, err := normalizeMigrationSource(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if got.String() != want {
			t.Fatalf("%q: got %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"", "ftp://host", "http://user:pass@host", "http://"} {
		if _, err := normalizeMigrationSource(input); err == nil {
			t.Fatalf("%q: expected an error", input)
		}
	}
}

func TestMigrationEndpointsRequireEnabledSourceAndToken(t *testing.T) {
	s := &Server{backup: &portableBackup{}}
	called := false
	handler := s.withMigrationToken(func(w http.ResponseWriter, r *http.Request) { called = true })
	request := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/migration/info", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	token, _, _ := s.migrationTokens.issue()
	if code := request(token); code != http.StatusNotFound || called {
		t.Fatalf("a server that is not a migration source must hide the endpoint, got %d", code)
	}
	s.config.MigrationSourceEnabled = true
	if code := request(""); code != http.StatusUnauthorized || called {
		t.Fatalf("missing token: got %d", code)
	}
	if code := request("mig_wrong"); code != http.StatusUnauthorized || called {
		t.Fatalf("wrong token: got %d", code)
	}
	if code := request(token); code != http.StatusOK || !called {
		t.Fatalf("valid token: got %d", code)
	}
}

func TestMigrationImportRejectedTokenExplainsItself(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid or expired migration token", http.StatusUnauthorized)
	}))
	defer source.Close()
	base, _ := url.Parse(source.URL)
	_, err := probeMigrationSource(context.Background(), migrationHTTPClient(), base, "mig_x")
	if err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("expected a token rejection, got %v", err)
	}
}

// A source that fails after the stream has started can no longer change the
// status code; the destination must still surface the source's reason rather
// than a bare "unexpected EOF".
func TestMigrationImportReportsSourceFailureFromTrailer(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", migrationErrorTrailer)
		w.WriteHeader(http.StatusOK)
		gz := gzip.NewWriter(w)
		_, _ = gz.Write(bytes.Repeat([]byte("x"), 4096))
		_ = gz.Flush() // truncated: never closed
		w.Header().Set(migrationErrorTrailer, "meme file \"a.png\" is missing from uploads")
	}))
	defer source.Close()

	dataDir := t.TempDir()
	s := &Server{backup: &portableBackup{
		dataDir:    dataDir,
		exportFunc: func(context.Context) (string, error) { return "", errors.New("no database in this test") },
	}}
	base, _ := url.Parse(source.URL)
	err := s.runMigrationImport(context.Background(), base, "mig_x")
	if err == nil || !strings.Contains(err.Error(), `meme file "a.png" is missing`) {
		t.Fatalf("expected the source's reason, got %v", err)
	}
	if received := s.migration.received.Load(); received == 0 {
		t.Fatal("expected the progress counter to record received bytes")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dataDir, ".memeindex-import-*"))
	if len(leftovers) != 0 {
		t.Fatalf("a failed migration must clean up its staging directory, found %v", leftovers)
	}
}

// TestMigrationEndToEnd pulls a real library from one server into another
// over HTTP, using the same handlers and import path as production.
func TestMigrationEndToEnd(t *testing.T) {
	sourceDB := newScratchBackupDatabase(t)
	destDB := newScratchBackupDatabase(t)
	ctx := context.Background()

	sourceDir := t.TempDir()
	memeBytes := bytes.Repeat([]byte("\x89PNG-migrated-"), 500)
	for path, payload := range map[string][]byte{
		filepath.Join(sourceDir, "uploads", "meme-1.png"):    memeBytes,
		filepath.Join(sourceDir, "thumbnails", "meme-1.jpg"): []byte("thumb"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := pgxpool.New(ctx, sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO memes (id, original_name, stored_name, file_path, content_type, size_bytes, created_at, updated_at)
		 VALUES ('meme-1', 'cat.png', 'meme-1.png', '/uploads/meme-1.png', 'image/png', 7000, NOW(), NOW())`,
		`INSERT INTO tags (name) VALUES ('migrated')`,
		`INSERT INTO meme_tags (meme_id, tag_id) SELECT 'meme-1', id FROM tags WHERE name = 'migrated'`,
	} {
		if _, err := seed.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed.Close()

	source := &Server{backup: newPortableBackup(sourceDB, sourceDir)}
	source.config.MigrationSourceEnabled = true
	token, _, _ := source.migrationTokens.issue()
	mux := http.NewServeMux()
	mux.Handle("/api/migration/info", source.withMigrationToken(source.handleMigrationInfo))
	mux.Handle("/api/migration/export", source.withMigrationToken(source.handleMigrationExport))
	sourceHTTP := httptest.NewServer(mux)
	defer sourceHTTP.Close()
	base, _ := url.Parse(sourceHTTP.URL)

	info, err := probeMigrationSource(ctx, migrationHTTPClient(), base, token)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Memes != 1 || info.EstimatedBytes < int64(len(memeBytes)) {
		t.Fatalf("unexpected source info %+v", info)
	}

	destDir := t.TempDir()
	dest := &Server{backup: newPortableBackup(destDB, destDir)}
	dest.migration.status.EstimatedBytes = info.EstimatedBytes
	if err := dest.runMigrationImport(ctx, base, token); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if dest.migration.received.Load() == 0 {
		t.Fatal("expected bytes to be counted")
	}
	// A token that has pulled the whole library once must not work again.
	if source.migrationTokens.valid(token) {
		t.Fatal("migration token still valid after a completed export")
	}
	if _, err := probeMigrationSource(ctx, migrationHTTPClient(), base, token); err == nil {
		t.Fatal("source still accepted the token after a completed export")
	}

	got, err := os.ReadFile(filepath.Join(destDir, "uploads", "meme-1.png"))
	if err != nil || !bytes.Equal(got, memeBytes) {
		t.Fatalf("migrated meme file mismatch: %v", err)
	}
	check, err := pgxpool.New(ctx, destDB)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var tag string
	if err := check.QueryRow(ctx, `SELECT t.name FROM meme_tags mt JOIN tags t ON t.id = mt.tag_id WHERE mt.meme_id = 'meme-1'`).Scan(&tag); err != nil || tag != "migrated" {
		t.Fatalf("migrated tag = %q, %v", tag, err)
	}
}
