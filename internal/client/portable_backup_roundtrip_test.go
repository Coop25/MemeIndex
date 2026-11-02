package client

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"memeindex/internal/dbschema"
)

// newScratchBackupDatabase creates a throwaway database for a backup round
// trip and returns its URL. A restore truncates every application table, so
// it must never run against the database other integration tests share.
// Skipped unless MEMEINDEX_TEST_DATABASE_URL is set (see
// internal/accessor/postgres_store_integration_test.go).
func newScratchBackupDatabase(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(admin.Close)
	name := fmt.Sprintf("memeindex_backup_rt_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.Path = "/" + name
	scratch := parsed.String()

	pool, err := pgxpool.New(ctx, scratch)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	defer pool.Close()
	var schema []string
	for i := 1; i <= 28; i++ {
		if i == 11 {
			continue // pg_trgm indexes: optional, irrelevant to backups
		}
		matches, err := filepath.Glob(filepath.Join("..", "dbschema", "sql", fmt.Sprintf("%03d_*.sql", i)))
		if err != nil || len(matches) != 1 {
			t.Fatalf("find migration %03d: %v %v", i, matches, err)
		}
		schema = append(schema, filepath.Base(matches[0]))
	}
	if err := dbschema.Apply(ctx, pool, schema...); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return scratch
}

// TestPortableBackupRoundTripThroughParts exports a real server into many
// small parts, changes the server, then restores it by staging the manifest
// and uploading the parts out of order - the same path the admin page uses.
func TestPortableBackupRoundTripThroughParts(t *testing.T) {
	databaseURL := newScratchBackupDatabase(t)
	ctx := context.Background()
	dataDir := t.TempDir()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	memeBytes := bytes.Repeat([]byte("\x89PNG-meme-"), 200)
	bookBytes := bytes.Repeat([]byte("m4b-audio-"), 300)
	bookPath := filepath.Join(dataDir, "audiobooks", "book-1", "book.m4b")
	for path, payload := range map[string][]byte{
		filepath.Join(dataDir, "uploads", "meme-1.png"):    memeBytes,
		filepath.Join(dataDir, "thumbnails", "meme-1.jpg"): []byte("thumb"),
		bookPath: bookBytes,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO memes (id, original_name, stored_name, file_path, content_type, size_bytes, created_at, updated_at)
		 VALUES ('meme-1', 'cat.png', 'meme-1.png', '/uploads/meme-1.png', 'image/png', 2000, NOW(), NOW())`,
		`INSERT INTO tags (name) VALUES ('cats')`,
		`INSERT INTO meme_tags (meme_id, tag_id) SELECT 'meme-1', id FROM tags WHERE name = 'cats'`,
		fmt.Sprintf(`INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, uploaded_by_user_id)
		 VALUES ('book-1', 'A Book', 'book.m4b', 3000, 60, '%s', 'user-1')`, bookPath),
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}

	source := newPortableBackup(databaseURL, dataDir)
	source.partMaxBytes = 512
	exportDir, err := source.export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := source.installExport(exportDir); err != nil {
		t.Fatalf("install export: %v", err)
	}
	status := source.exportStatus()
	if len(status.Parts) < 3 {
		t.Fatalf("expected the export split into several parts, got %d", len(status.Parts))
	}
	manifest, err := os.ReadFile(filepath.Join(source.latestDir, status.Filename))
	if err != nil {
		t.Fatal(err)
	}
	parts := make([][]byte, len(status.Parts))
	for i, part := range status.Parts {
		if parts[i], err = os.ReadFile(filepath.Join(source.latestDir, part.Filename)); err != nil {
			t.Fatal(err)
		}
	}

	// Diverge from the backup: drop the meme and its file, add a stray tag.
	for _, statement := range []string{`DELETE FROM memes WHERE id = 'meme-1'`, `INSERT INTO tags (name) VALUES ('stray')`} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(dataDir, "uploads", "meme-1.png")); err != nil {
		t.Fatal(err)
	}

	if _, err := source.stageImportManifest(manifest); err != nil {
		t.Fatalf("stage manifest: %v", err)
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if _, err := source.receiveImportPart(status.Parts[i].Filename, bytes.NewReader(parts[i])); err != nil {
			t.Fatalf("receive part %d: %v", i+1, err)
		}
	}
	if err := source.importStagedParts(ctx); err != nil {
		t.Fatalf("restore from parts: %v", err)
	}

	var tagNames string
	if err := pool.QueryRow(ctx, `SELECT string_agg(t.name, ',' ORDER BY t.name) FROM tags t`).Scan(&tagNames); err != nil {
		t.Fatal(err)
	}
	var taggedMemes int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM meme_tags WHERE meme_id = 'meme-1'`).Scan(&taggedMemes); err != nil {
		t.Fatal(err)
	}
	if tagNames != "cats" || taggedMemes != 1 {
		t.Fatalf("database not restored: tags=%q tagged memes=%d", tagNames, taggedMemes)
	}
	for path, want := range map[string][]byte{filepath.Join(dataDir, "uploads", "meme-1.png"): memeBytes, bookPath: bookBytes} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s not restored (err %v)", path, err)
		}
	}
	if staged, err := source.incomingStatus(); err != nil || staged.Staged {
		t.Fatalf("staged parts should be cleared after restore: %+v err=%v", staged, err)
	}
	snapshots, err := os.ReadDir(source.preRestoreDir())
	if err != nil || len(snapshots) != 1 || !snapshots[0].IsDir() {
		t.Fatalf("expected one pre-restore snapshot directory, got %v (err %v)", snapshots, err)
	}
}
