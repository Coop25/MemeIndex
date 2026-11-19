package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storedTestArchive is makeTestArchive written with gzip.NoCompression: the
// deflate stream is stored blocks, which carry no check of their own, so only
// the gzip trailer's CRC can tell that bytes inside them changed.
func storedTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	gz, err := gzip.NewWriterLevel(&output, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	for name, payload := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// A restore has to notice a backup whose bytes changed in storage or transit.
// The tar reader stops at the end-of-archive marker, before the gzip trailer,
// so unless the rest of the gzip stream is read its CRC is never checked: a
// flipped bit in a stored block was restored as a silently different file.
func TestExtractRejectsArchiveFailingGzipChecksum(t *testing.T) {
	const payload = "MEME-PAYLOAD-0123456789-the quick brown fox"
	files := map[string]string{"manifest.json": "{}", "uploads/a.txt": payload}

	flippedPayload := storedTestArchive(t, files)
	at := bytes.Index(flippedPayload, []byte(payload))
	if at < 0 {
		t.Fatal("payload not stored verbatim; the archive is not using stored blocks")
	}
	flippedPayload[at] ^= 0x01

	flippedCRC := storedTestArchive(t, files)
	flippedCRC[len(flippedCRC)-8] ^= 0x01 // first byte of the CRC-32 trailer

	missingTrailer := storedTestArchive(t, files)
	missingTrailer = missingTrailer[:len(missingTrailer)-8]

	for name, archive := range map[string][]byte{
		"flipped payload byte": flippedPayload,
		"flipped crc":          flippedCRC,
		"missing trailer":      missingTrailer,
	} {
		t.Run(name, func(t *testing.T) {
			err := extractPortableArchive(bytes.NewReader(archive), t.TempDir())
			if err == nil {
				t.Fatal("extract accepted an archive whose gzip checksum does not match")
			}
			var friendly *userFacingError
			if !errors.As(err, &friendly) || !strings.Contains(friendly.Error(), "truncated or damaged") {
				t.Fatalf("extract error %q is not reported as a damaged backup", err)
			}
		})
	}
}

// Draining the gzip stream reads to the end of the one gzip member the
// archive is in, not past it, so bytes after that member - which no restore
// ever read before - still don't fail it.
func TestExtractIgnoresBytesAfterTheGzipStream(t *testing.T) {
	archive := storedTestArchive(t, map[string]string{"manifest.json": "{}", "uploads/a.txt": "hello"})
	archive = append(archive, "trailing junk that is not gzip"...)
	dir := t.TempDir()
	if err := extractPortableArchive(bytes.NewReader(archive), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "uploads", "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("uploads/a.txt = %q, %v", got, err)
	}
}
