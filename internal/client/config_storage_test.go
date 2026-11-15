package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateStorageRequiresDatabaseURL(t *testing.T) {
	err := Config{DataDir: t.TempDir()}.validateStorage()
	if err == nil || !strings.Contains(err.Error(), "MEMEINDEX_DATABASE_URL is required") {
		t.Fatalf("err = %v, want a missing database URL error", err)
	}
	if strings.Contains(err.Error(), "JSON store data") {
		t.Fatalf("err = %v mentions JSON data for an empty data dir", err)
	}
}

// An instance that ran on the old local JSON store is told its data is still
// there and how Postgres picks it up, instead of starting with an empty vault.
func TestValidateStorageNamesLegacyJSONStoreFiles(t *testing.T) {
	dataDir := t.TempDir()
	for _, name := range []string{"index.json", "favorites.json"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("[]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := Config{DataDir: dataDir}.validateStorage()
	if err == nil {
		t.Fatal("expected an error without MEMEINDEX_DATABASE_URL")
	}
	for _, want := range []string{"index.json", "favorites.json", "imports it on first start"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want it to mention %q", err, want)
		}
	}
}

func TestValidateStorageAcceptsDatabaseURL(t *testing.T) {
	if err := (Config{DataDir: t.TempDir(), DatabaseURL: "postgres://localhost/memeindex"}).validateStorage(); err != nil {
		t.Fatalf("validateStorage: %v", err)
	}
}
