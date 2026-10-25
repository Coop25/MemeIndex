package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

// errAudiobookNotMP3 is remuxStoredMP3's "nothing to do" result, as opposed
// to a remux that was attempted and failed.
var errAudiobookNotMP3 = errors.New("audiobook is not an mp3")

// audiobookRemuxRetryAfter is how long a book whose remux failed is left on
// its MP3 before another attempt. Without it, a file ffmpeg can never convert
// (corrupt, or an ID3-tagged non-MP3 that sniffs as audio/mpeg) would get a
// full read-and-rewrite on every server start, forever.
const audiobookRemuxRetryAfter = 7 * 24 * time.Hour

// remuxStoredAudiobookMP3s converts every audiobook still stored as an MP3 to
// a lossless .m4b - the same conversion every MP3 upload gets once it
// completes - so books uploaded before it seek accurately too.
func (s *Server) remuxStoredAudiobookMP3s(ctx context.Context) {
	books, err := s.audiobooks.ListStoredMP3s(ctx)
	if err != nil {
		log.Printf("audiobooks: mp3 remux backfill failed: %v", err)
		s.audiobookRemuxProgress.finish(err)
		return
	}
	s.audiobookRemuxProgress.begin(books)

	converted := 0
	for _, book := range books {
		if s.remuxAudiobookMP3(ctx, book) {
			converted++
		}
	}
	s.audiobookRemuxProgress.finish(nil)
	if converted > 0 {
		log.Printf("audiobooks: remuxed %d existing mp3 audiobook(s) to m4b for accurate seeking", converted)
	}
}

// remuxUploadedAudiobookMP3 converts a just-completed MP3 upload in the
// background. It runs after the upload request has already returned rather
// than inside it: a long book takes a minute or more, longer than a proxy in
// front (Cloudflare's tunnel allows 100 s) will hold a response open, and the
// book is playable as an MP3 in the meantime.
func (s *Server) remuxUploadedAudiobookMP3(record audiobookRecord) {
	if !isAudiobookMP3(record.StoragePath, record.MimeType) {
		return
	}
	book := audiobookStoredMP3{
		ID: record.ID, Title: record.Title, Filename: record.Filename,
		StoragePath: record.StoragePath, MimeType: record.MimeType, SizeBytes: record.SizeBytes,
	}
	s.audiobookRemuxProgress.add(book)
	s.remuxAudiobookMP3(context.Background(), book)
}

// remuxAudiobookMP3 converts one MP3 audiobook and reports whether it did.
// Remuxes run one at a time across the startup backfill and uploads: that
// caps the extra disk to one book's worth, and keeps two of them from ever
// writing the same .m4b (the backfill's list can include an upload that
// finished while it was starting up). The row is re-read under the lock for
// the same reason - whichever got there first has already converted it.
func (s *Server) remuxAudiobookMP3(ctx context.Context, book audiobookStoredMP3) bool {
	s.audiobookRemuxMu.Lock()
	defer s.audiobookRemuxMu.Unlock()

	if failedAt, reason, ok := recentAudiobookRemuxFailure(book.StoragePath); ok {
		s.audiobookRemuxProgress.itemSkipped(book.ID, fmt.Sprintf("failed %s, retried after %s: %s",
			failedAt.Format(time.DateTime), failedAt.Add(audiobookRemuxRetryAfter).Format(time.DateTime), reason))
		return false
	}
	current, err := s.audiobooks.GetAudiobook(ctx, book.ID)
	if errors.Is(err, ErrAudiobookNotFound) {
		s.audiobookRemuxProgress.itemSkipped(book.ID, "deleted before it was converted")
		return false
	}
	if err == nil && current.StoragePath != book.StoragePath {
		s.audiobookRemuxProgress.itemSkipped(book.ID, "already converted")
		return false
	}

	s.audiobookRemuxProgress.itemStarted(book.ID, remuxedM4BPath(book.StoragePath))
	size, err := s.swapAudiobookToRemuxedM4B(ctx, book)
	s.audiobookRemuxProgress.itemFinished(book.ID, size, err)
	if err != nil {
		log.Printf("audiobooks: remux of %s (%s) to m4b failed (keeping the mp3): %v", book.ID, book.Title, err)
		recordAudiobookRemuxFailure(book.StoragePath, err)
		return false
	}
	return true
}

func (s *Server) swapAudiobookToRemuxedM4B(ctx context.Context, book audiobookStoredMP3) (int64, error) {
	m4bPath, size, err := remuxStoredMP3(book.StoragePath, book.MimeType)
	if err != nil {
		return 0, err
	}
	if err := s.audiobooks.RecordRemuxedM4B(ctx, book, m4bPath, size); err != nil {
		_ = os.Remove(m4bPath)
		if errors.Is(err, ErrAudiobookNotFound) {
			return 0, errors.New("audiobook was deleted or replaced during the remux")
		}
		return 0, err
	}
	// A listener mid-book keeps requesting the same URL, which now serves the
	// .m4b. A browser that rejects the changed file raises a media error and
	// the player reloads its source at the same position (see the audio
	// element's error handler in app.js). Chrome instead resyncs on the very
	// same MP3 frames inside the .m4b and plays on, still mapping time to
	// bytes the MP3 way - so until the book is reopened its position is off
	// by about the size of the new index, much as VBR MP3 seeking already was.
	removeRemuxedMP3(book.StoragePath)
	clearAudiobookRemuxFailure(book.StoragePath)
	return size, nil
}

// remuxStoredMP3 writes a lossless .m4b copy of a stored MP3 next to it (see
// accessor.RemuxMP3ToM4B for why), returning the new path and size. The MP3
// is left in place for the caller to delete once nothing references it. On
// any error no .m4b is left behind; errAudiobookNotMP3 means the file isn't
// an MP3 at all (sniffing only recognizes one with an ID3 tag, hence the
// extension check too).
func remuxStoredMP3(mp3Path, mimeType string) (string, int64, error) {
	if !isAudiobookMP3(mp3Path, mimeType) {
		return "", 0, errAudiobookNotMP3
	}
	m4bPath := remuxedM4BPath(mp3Path)
	if err := accessor.RemuxMP3ToM4B(mp3Path, m4bPath); err != nil {
		return "", 0, err
	}
	info, err := os.Stat(m4bPath)
	if err != nil {
		_ = os.Remove(m4bPath)
		return "", 0, fmt.Errorf("stat remuxed file: %w", err)
	}
	return m4bPath, info.Size(), nil
}

func isAudiobookMP3(path, mimeType string) bool {
	return mimeType == "audio/mpeg" || strings.EqualFold(filepath.Ext(path), ".mp3")
}

func remuxedM4BPath(mp3Path string) string {
	return strings.TrimSuffix(mp3Path, filepath.Ext(mp3Path)) + ".m4b"
}

func removeRemuxedMP3(mp3Path string) {
	if err := os.Remove(mp3Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("audiobooks: could not remove %s after remux: %v", filepath.Base(mp3Path), err)
	}
}

// A failed remux is remembered in a small marker file beside the MP3 - its
// modification time is when it failed, its contents why - rather than in a
// database column, since it only matters while that MP3 exists.
func audiobookRemuxFailurePath(mp3Path string) string {
	return mp3Path + ".remux-failed"
}

func recordAudiobookRemuxFailure(mp3Path string, cause error) {
	if err := os.WriteFile(audiobookRemuxFailurePath(mp3Path), []byte(cause.Error()), 0o644); err != nil {
		log.Printf("audiobooks: could not record remux failure for %s: %v", filepath.Base(mp3Path), err)
	}
}

func clearAudiobookRemuxFailure(mp3Path string) {
	_ = os.Remove(audiobookRemuxFailurePath(mp3Path))
}

// recentAudiobookRemuxFailure reports a failure recorded within
// audiobookRemuxRetryAfter; an older one is cleared so the book is retried.
func recentAudiobookRemuxFailure(mp3Path string) (time.Time, string, bool) {
	marker := audiobookRemuxFailurePath(mp3Path)
	info, err := os.Stat(marker)
	if err != nil {
		return time.Time{}, "", false
	}
	if time.Since(info.ModTime()) >= audiobookRemuxRetryAfter {
		_ = os.Remove(marker)
		return time.Time{}, "", false
	}
	reason, _ := os.ReadFile(marker)
	return info.ModTime(), strings.TrimSpace(string(reason)), true
}
