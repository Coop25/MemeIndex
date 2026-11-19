package client

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"memeindex/internal/accessor"
)

// linkImportResponseBudget is how long a link-import request waits for the
// download before answering "still working on it" (202, pending) and letting
// the import finish in the background. It stays well inside Cloudflare's
// 100-second proxy timeout, which otherwise turned a slow-but-successful
// YouTube download into a 524 error page while the server kept going - and a
// user who retried then got a confusing duplicate. A var so tests can shrink it.
var linkImportResponseBudget = 75 * time.Second

// userFacingError marks an error whose message is written for end users and
// is safe to send back in a response. Anything else (yt-dlp stderr, DNS and
// dial errors, file paths) is logged and replaced with a generic message.
type userFacingError struct {
	message string
	// cause, when set, is the underlying error behind message. It shows in
	// Error() - and so in the server log - but never in clientSafeError.
	cause error
}

func (e *userFacingError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *userFacingError) Unwrap() error { return e.cause }

func userError(message string) error { return &userFacingError{message: message} }

// userErrorWrap is userError for a failure with an underlying cause worth
// logging (a parser error, a database error) that must not reach the client.
func userErrorWrap(message string, cause error) error {
	return &userFacingError{message: message, cause: cause}
}

// clientSafeError returns err's message when it is a userFacingError, else
// fallback.
func clientSafeError(err error, fallback string) string {
	var friendly *userFacingError
	if errors.As(err, &friendly) {
		return friendly.message
	}
	return fallback
}

type linkImportOutcome struct {
	meme accessor.Meme
	err  error
}

// importLinkWithinBudget runs createMemeFromSourceURL detached from the
// request, so the download survives the response. It returns the result if the
// import finishes within linkImportResponseBudget; otherwise pending is true
// and finishBackgroundLinkImport takes over (tag suggestions on success, the
// retry queue on a retryable failure). Both goroutines are tracked by
// s.background so shutdown waits for them (and, past its grace period, cancels
// the download) before the store is closed. The import is also one unit of
// restore-pausable work, so a restore that begins while it is downloading
// waits for it to be saved instead of having it land mid-restore.
func (s *Server) importLinkWithinBudget(r *http.Request, actor accessor.AuditActor, sourceURL string, tags []string, notes string) (meme accessor.Meme, err error, pending bool) {
	done := make(chan linkImportOutcome, 1)
	requestValues := context.WithoutCancel(r.Context())
	started := s.background.Go(func(stop context.Context) {
		if !s.enterBackgroundJob() {
			done <- linkImportOutcome{err: userError("The server is restarting. Please try again in a moment.")}
			return
		}
		defer s.leaveBackgroundJob()
		ctx, cancel := context.WithCancel(requestValues)
		defer cancel()
		defer context.AfterFunc(stop, cancel)()
		meme, err := s.createMemeFromSourceURL(ctx, actor, sourceURL, tags, notes)
		done <- linkImportOutcome{meme: meme, err: err}
	})
	if !started {
		return accessor.Meme{}, userError("The server is restarting. Please try again in a moment."), false
	}

	timer := time.NewTimer(linkImportResponseBudget)
	defer timer.Stop()
	select {
	case out := <-done:
		return out.meme, out.err, false
	case <-timer.C:
	case <-r.Context().Done():
	}
	if !s.background.Go(func(ctx context.Context) {
		s.finishBackgroundLinkImport(ctx, done, actor, sourceURL, tags, notes)
	}) {
		log.Printf("background link import of %q will not be followed up: server is shutting down", sanitizeLogField(sourceURL))
	}
	return accessor.Meme{}, nil, true
}

func (s *Server) finishBackgroundLinkImport(ctx context.Context, done <-chan linkImportOutcome, actor accessor.AuditActor, sourceURL string, tags []string, notes string) {
	out := <-done
	if out.err == nil {
		s.managers.QueueMemeTagSuggestions(ctx, out.meme.ID)
		log.Printf("background link import of %q finished", sanitizeLogField(sourceURL))
		return
	}
	var duplicateErr *accessor.DuplicateMemeError
	if errors.As(out.err, &duplicateErr) {
		return
	}
	if s.shouldQueueLinkRetry(sourceURL, out.err) {
		s.linkRetries.Enqueue(sourceURL, tags, notes, actor)
		log.Printf("background link import of %q failed, queued for retry: %v", sanitizeLogField(sourceURL), out.err)
		return
	}
	log.Printf("background link import of %q failed: %v", sanitizeLogField(sourceURL), out.err)
}
