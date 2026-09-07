package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeDownloadFilename(t *testing.T) {
	cases := map[string]string{
		"memeindex-backup-20240101-000000.tar.gz": "memeindex-backup-20240101-000000.tar.gz",
		`evil".tar.gz`:                             "evil.tar.gz",
		"with\r\nnewline.tar.gz":                   "withnewline.tar.gz",
		"../../etc/passwd":                         "passwd",
		"":                                        "",
		"..":                                      "",
	}
	for input, want := range cases {
		if got := sanitizeDownloadFilename(input); got != want {
			t.Errorf("sanitizeDownloadFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPruneOldPreRestoreSnapshots(t *testing.T) {
	dir := t.TempDir()
	b := &portableBackup{dataDir: dir}
	if err := os.MkdirAll(b.preRestoreDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	names := []string{
		"memeindex-pre-restore-20240101-000000.tar.gz",
		"memeindex-pre-restore-20240102-000000.tar.gz",
		"memeindex-pre-restore-20240103-000000.tar.gz",
		"memeindex-pre-restore-20240104-000000.tar.gz",
		"memeindex-pre-restore-20240105-000000.tar.gz",
		"unrelated.txt",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(b.preRestoreDir(), name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	b.pruneOldPreRestoreSnapshots(2)

	entries, err := os.ReadDir(b.preRestoreDir())
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Name()] = true
	}
	if !got["memeindex-pre-restore-20240104-000000.tar.gz"] || !got["memeindex-pre-restore-20240105-000000.tar.gz"] {
		t.Errorf("expected the two newest snapshots to survive, got %v", got)
	}
	if got["memeindex-pre-restore-20240101-000000.tar.gz"] {
		t.Errorf("oldest snapshot was not pruned: %v", got)
	}
	if !got["unrelated.txt"] {
		t.Errorf("prune removed an unrelated file")
	}
}
