package client

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// remuxStoredAudiobookMP3s converts every audiobook still stored as an MP3
// to a lossless .m4b - the same conversion CompleteUpload does for new
// uploads - so books uploaded before it seek accurately too. Books go one at
// a time, so the extra disk needed is one book's worth. A failure leaves that
// book on its MP3, and it is retried on the next start.
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
		s.audiobookRemuxProgress.itemStarted(book.ID, strings.TrimSuffix(book.StoragePath, filepath.Ext(book.StoragePath))+".m4b")
		size, err := s.remuxStoredAudiobookMP3(ctx, book)
		s.audiobookRemuxProgress.itemFinished(book.ID, size, err)
		if err != nil {
			log.Printf("audiobooks: remux of %s (%s) to m4b failed (keeping the mp3): %v", book.ID, book.Title, err)
			continue
		}
		converted++
	}
	s.audiobookRemuxProgress.finish(nil)
	if converted > 0 {
		log.Printf("audiobooks: remuxed %d existing mp3 audiobook(s) to m4b for accurate seeking", converted)
	}
}

func (s *Server) remuxStoredAudiobookMP3(ctx context.Context, book audiobookStoredMP3) (int64, error) {
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
	// Unlinking is safe even mid-stream on Linux: a listener still on the old
	// URL keeps its open handle, and their next request gets the .m4b.
	removeRemuxedMP3(book.StoragePath)
	return size, nil
}
