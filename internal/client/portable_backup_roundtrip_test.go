package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		`UPDATE memes SET share_generation = 3, share_expires_at = '2027-01-01T00:00:00Z', shared_at = NOW(), shared_by_user_id = 'user-1' WHERE id = 'meme-1'`,
		`INSERT INTO app_users (user_id, session_version) VALUES ('user-1', 7)`,
		`INSERT INTO app_user_readd_required (user_id, session_version) VALUES ('user-2', 4)`,
		`INSERT INTO tags (name) VALUES ('cats')`,
		`INSERT INTO meme_tags (meme_id, tag_id) SELECT 'meme-1', id FROM tags WHERE name = 'cats'`,
		fmt.Sprintf(`INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, uploaded_by_user_id)
		 VALUES ('book-1', 'A Book', 'book.m4b', 3000, 60, '%s', 'user-1')`, bookPath),
		`INSERT INTO audiobook_progress (audiobook_id, user_id, position_seconds, updated_at, finished_at)
		 VALUES ('book-1', 'user-1', 59, NOW(), '2026-10-01T12:00:00Z')`,
		`INSERT INTO audiobook_listening_stats (user_id, audiobook_id, listen_date, seconds_listened)
		 VALUES ('user-1', 'book-1', '2026-10-01', 1234.5)`,
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

	// Diverge from the backup: drop the meme and its file, add a stray tag,
	// and lose the listening history.
	for _, statement := range []string{
		`DELETE FROM memes WHERE id = 'meme-1'`,
		`INSERT INTO tags (name) VALUES ('stray')`,
		`DELETE FROM audiobook_listening_stats`,
		`UPDATE audiobook_progress SET finished_at = NULL`,
		`UPDATE app_users SET session_version = 1`,
		`UPDATE app_user_readd_required SET session_version = 1`,
	} {
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
	// Session versions and share links survive: resetting the first would let
	// a logged-out token validate again, dropping the second kills every link.
	var sessionVersion, readdVersion, shareGeneration int64
	var shareExpiresAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT session_version FROM app_users WHERE user_id = 'user-1'`).Scan(&sessionVersion); err != nil {
		t.Fatalf("read restored session version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT session_version FROM app_user_readd_required WHERE user_id = 'user-2'`).Scan(&readdVersion); err != nil {
		t.Fatalf("read restored re-add session version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT share_generation, share_expires_at FROM memes WHERE id = 'meme-1'`).Scan(&shareGeneration, &shareExpiresAt); err != nil {
		t.Fatalf("read restored share state: %v", err)
	}
	if sessionVersion != 7 || readdVersion != 4 {
		t.Fatalf("session versions not restored: app_users=%d readd=%d, want 7 and 4", sessionVersion, readdVersion)
	}
	if want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC); shareGeneration != 3 || shareExpiresAt == nil || !shareExpiresAt.Equal(want) {
		t.Fatalf("share link not restored: generation=%d expires=%v", shareGeneration, shareExpiresAt)
	}
	var finishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT finished_at FROM audiobook_progress WHERE audiobook_id = 'book-1' AND user_id = 'user-1'`).Scan(&finishedAt); err != nil {
		t.Fatalf("read restored progress: %v", err)
	}
	if want := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC); finishedAt == nil || !finishedAt.Equal(want) {
		t.Fatalf("finished_at not restored: got %v, want %v", finishedAt, want)
	}
	var listenDate string
	var secondsListened float64
	if err := pool.QueryRow(ctx, `SELECT listen_date::text, seconds_listened FROM audiobook_listening_stats WHERE user_id = 'user-1' AND audiobook_id = 'book-1'`).Scan(&listenDate, &secondsListened); err != nil {
		t.Fatalf("listening stats not restored: %v", err)
	}
	if listenDate != "2026-10-01" || secondsListened != 1234.5 {
		t.Fatalf("listening stats restored wrong: date=%s seconds=%v", listenDate, secondsListened)
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

// TestPortableBackupRestoresFormat3Archive checks that a backup taken before
// listening stats existed (format 3: no audiobook_listening_stats.csv, and
// audiobook_progress without finished_at) still restores.
func TestPortableBackupRestoresFormat3Archive(t *testing.T) {
	databaseURL := newScratchBackupDatabase(t)
	ctx := context.Background()
	dataDir := t.TempDir()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	bookPath := filepath.Join(dataDir, "audiobooks", "book-1", "book.m4b")
	if err := os.MkdirAll(filepath.Dir(bookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bookPath, []byte("m4b-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		fmt.Sprintf(`INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, uploaded_by_user_id)
		 VALUES ('book-1', 'A Book', 'book.m4b', 9, 60, '%s', 'user-1')`, bookPath),
		`INSERT INTO audiobook_progress (audiobook_id, user_id, position_seconds, updated_at, finished_at)
		 VALUES ('book-1', 'user-1', 42, NOW(), NOW())`,
		`INSERT INTO audiobook_listening_stats (user_id, audiobook_id, listen_date, seconds_listened)
		 VALUES ('user-1', 'book-1', '2026-10-01', 10)`,
		`INSERT INTO app_users (user_id, session_version) VALUES ('user-1', 7)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}

	backup := newPortableBackup(databaseURL, dataDir)
	exportDir, err := backup.export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer os.RemoveAll(exportDir)
	parts, err := readExportParts(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, filepath.Join(exportDir, part.Filename))
	}
	reader, closeParts, err := openPartFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	archive := downgradeArchiveToFormat3(t, reader)
	closeParts()

	if err := backup.importArchive(ctx, bytes.NewReader(archive)); err != nil {
		t.Fatalf("restore format-3 archive: %v", err)
	}
	var position float64
	var finishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT position_seconds, finished_at FROM audiobook_progress WHERE audiobook_id = 'book-1' AND user_id = 'user-1'`).Scan(&position, &finishedAt); err != nil {
		t.Fatalf("read restored progress: %v", err)
	}
	if position != 42 || finishedAt != nil {
		t.Fatalf("format-3 progress restored wrong: position=%v finished_at=%v", position, finishedAt)
	}
	var stats int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audiobook_listening_stats`).Scan(&stats); err != nil {
		t.Fatal(err)
	}
	if stats != 0 {
		t.Fatalf("format-3 restore should leave listening stats empty, found %d rows", stats)
	}
	// The archive never recorded session versions, so the restored account
	// must land past any version an old token could carry, not back at 1.
	var sessionVersion int64
	if err := pool.QueryRow(ctx, `SELECT session_version FROM app_users WHERE user_id = 'user-1'`).Scan(&sessionVersion); err != nil {
		t.Fatalf("read restored session version: %v", err)
	}
	if sessionVersion <= 7 {
		t.Fatalf("legacy restore left session_version at %d; pre-restore tokens could validate again", sessionVersion)
	}
}

// downgradeArchiveToFormat3 rewrites a current export the way v2.27.0 would
// have written it: format_version 3, no listening stats CSV,
// audiobook_progress without its trailing finished_at column, and none of the
// session-version or share-link columns format 5 added.
func downgradeArchiveToFormat3(t *testing.T, source io.Reader) []byte {
	t.Helper()
	gzr, err := gzip.NewReader(source)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gzr)
	var output bytes.Buffer
	gzw := gzip.NewWriter(&output)
	tw := tar.NewWriter(gzw)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		// Columns format 5 appended, which a format-3 archive never had.
		if dropped, ok := map[string]int{"database/memes.csv": 4, "database/app_users.csv": 1, "database/app_user_readd_required.csv": 1}[header.Name]; ok {
			records, err := csv.NewReader(bytes.NewReader(payload)).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			var rewritten bytes.Buffer
			w := csv.NewWriter(&rewritten)
			for _, record := range records {
				if err := w.Write(record[:len(record)-dropped]); err != nil {
					t.Fatal(err)
				}
			}
			w.Flush()
			payload = rewritten.Bytes()
		}
		switch header.Name {
		case "database/audiobook_listening_stats.csv":
			continue
		case "manifest.json":
			var manifest map[string]any
			if err := json.Unmarshal(payload, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["format_version"] = 3
			if payload, err = json.Marshal(manifest); err != nil {
				t.Fatal(err)
			}
		case "database/audiobook_progress.csv":
			records, err := csv.NewReader(bytes.NewReader(payload)).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) == 0 || records[0][len(records[0])-1] != "finished_at" {
				t.Fatalf("expected finished_at to be the last progress column, got %v", records)
			}
			var rewritten bytes.Buffer
			w := csv.NewWriter(&rewritten)
			for _, record := range records {
				if err := w.Write(record[:len(record)-1]); err != nil {
					t.Fatal(err)
				}
			}
			w.Flush()
			payload = rewritten.Bytes()
		}
		header.Size = int64(len(payload))
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
