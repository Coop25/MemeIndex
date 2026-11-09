package client

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"memeindex/internal/accessor"
)

type LinkRetryJob struct {
	ID            string              `json:"id"`
	SourceURL     string              `json:"source_url"`
	Tags          []string            `json:"tags,omitempty"`
	Notes         string              `json:"notes,omitempty"`
	Actor         accessor.AuditActor `json:"actor"`
	RequestedAt   time.Time           `json:"requested_at"`
	NextAttemptAt time.Time           `json:"next_attempt_at"`
	Attempts      int                 `json:"attempts"`
	MaxAttempts   int                 `json:"max_attempts"`
	LastError     string              `json:"last_error,omitempty"`
}

type LinkRetryQueueStatus struct {
	RetryIntervalSeconds int            `json:"retry_interval_seconds"`
	MaxAttempts          int            `json:"max_attempts"`
	ProcessingID         string         `json:"processing_id,omitempty"`
	Queued               []LinkRetryJob `json:"queued,omitempty"`
	Rejected             []LinkRetryJob `json:"rejected,omitempty"`
}

type linkRetryQueue struct {
	interval    time.Duration
	maxAttempts int
	processFn   func(context.Context, LinkRetryJob) error

	mu           sync.Mutex
	queued       []LinkRetryJob
	rejected     []LinkRetryJob
	processingID string
	// processing is the job run is working on right now. It is written to
	// disk as still queued, so a restart mid-download retries it instead of
	// losing it.
	processing *LinkRetryJob
	// persistPath, when set, is the file the queue is saved to after every
	// change (see persistTo). Empty keeps the queue in memory only.
	persistPath string
}

// persistedLinkRetries is the on-disk form of the queue.
type persistedLinkRetries struct {
	Queued   []LinkRetryJob `json:"queued"`
	Rejected []LinkRetryJob `json:"rejected"`
}

// persistTo loads any queue previously saved at path and saves every later
// change there, so queued and rejected link imports survive a restart or
// deploy. A missing file is an empty queue; an unreadable one is reported and
// left in place rather than overwritten.
func (q *linkRetryQueue) persistTo(path string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	payload, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		var saved persistedLinkRetries
		if err := json.Unmarshal(payload, &saved); err != nil {
			return err
		}
		q.queued = append(q.queued, saved.Queued...)
		q.rejected = append(q.rejected, saved.Rejected...)
	}
	q.persistPath = path
	return nil
}

// saveLocked writes the queue to persistPath. Callers hold q.mu. A failed
// write is logged, not returned: the in-memory queue is still correct and the
// next change tries again.
func (q *linkRetryQueue) saveLocked() {
	if q.persistPath == "" {
		return
	}
	saved := persistedLinkRetries{
		Queued:   append([]LinkRetryJob{}, q.queued...),
		Rejected: append([]LinkRetryJob{}, q.rejected...),
	}
	if q.processing != nil {
		saved.Queued = append(saved.Queued, *q.processing)
	}
	payload, err := json.Marshal(saved)
	if err != nil {
		log.Printf("link retry queue: encode failed: %v", err)
		return
	}
	// Write beside the target and rename over it so a crash mid-write cannot
	// leave a truncated queue file.
	temp, err := os.CreateTemp(filepath.Dir(q.persistPath), ".link-retries-*.tmp")
	if err != nil {
		log.Printf("link retry queue: save failed: %v", err)
		return
	}
	_, writeErr := temp.Write(payload)
	closeErr := temp.Close()
	if writeErr == nil && closeErr == nil {
		writeErr = os.Rename(temp.Name(), q.persistPath)
	}
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(temp.Name())
		log.Printf("link retry queue: save failed: %v", errors.Join(writeErr, closeErr))
	}
}

// maxRejectedLinkRetries caps how many permanently failed jobs the queue keeps
// for the admin panel; past it the oldest are dropped so a long-running server
// with a broken downloader doesn't grow this list without bound.
const maxRejectedLinkRetries = 200

// linkRetryPollInterval is how often run checks for a ready job. A var so
// tests can shrink it.
var linkRetryPollInterval = 5 * time.Second

// newLinkRetryQueue builds an idle queue; the caller starts processing with
// run, normally as a tracked background task so shutdown can stop it.
func newLinkRetryQueue(interval time.Duration, maxAttempts int, processFn func(context.Context, LinkRetryJob) error) *linkRetryQueue {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	q := &linkRetryQueue{
		interval:    interval,
		maxAttempts: maxAttempts,
		processFn:   processFn,
	}
	return q
}

func (q *linkRetryQueue) Enqueue(sourceURL string, tags []string, notes string, actor accessor.AuditActor) LinkRetryJob {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	job := LinkRetryJob{
		ID:            uuid.Must(uuid.NewV7()).String(),
		SourceURL:     strings.TrimSpace(sourceURL),
		Tags:          append([]string(nil), tags...),
		Notes:         strings.TrimSpace(notes),
		Actor:         actor,
		RequestedAt:   now,
		NextAttemptAt: now.Add(q.interval),
		Attempts:      0,
		MaxAttempts:   q.maxAttempts,
	}
	q.queued = append(q.queued, job)
	q.saveLocked()
	return job
}

func (q *linkRetryQueue) Snapshot() LinkRetryQueueStatus {
	q.mu.Lock()
	defer q.mu.Unlock()

	queued := append([]LinkRetryJob(nil), q.queued...)
	rejected := append([]LinkRetryJob(nil), q.rejected...)
	slices.SortFunc(queued, func(left, right LinkRetryJob) int {
		return left.NextAttemptAt.Compare(right.NextAttemptAt)
	})
	slices.SortFunc(rejected, func(left, right LinkRetryJob) int {
		return right.RequestedAt.Compare(left.RequestedAt)
	})

	return LinkRetryQueueStatus{
		RetryIntervalSeconds: int(q.interval / time.Second),
		MaxAttempts:          q.maxAttempts,
		ProcessingID:         q.processingID,
		Queued:               queued,
		Rejected:             rejected,
	}
}

func (q *linkRetryQueue) RequeueRejected(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	id = strings.TrimSpace(id)
	for index, job := range q.rejected {
		if job.ID != id {
			continue
		}
		job.Attempts = 0
		job.LastError = ""
		job.NextAttemptAt = time.Now().UTC().Add(q.interval)
		q.rejected = append(q.rejected[:index], q.rejected[index+1:]...)
		q.queued = append(q.queued, job)
		q.saveLocked()
		return true
	}
	return false
}

// run processes ready jobs until quit is closed. A job in progress when quit
// closes is allowed to finish; ctx cancels it if shutdown runs out of time.
func (q *linkRetryQueue) run(ctx context.Context, quit <-chan struct{}) {
	ticker := time.NewTicker(linkRetryPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
		}

		job, ok := q.dequeueReady()
		if !ok {
			continue
		}

		jobCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		err := q.processFn(jobCtx, job)
		cancel()
		q.finish(job, err)
	}
}

func (q *linkRetryQueue) dequeueReady() (LinkRetryJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.processingID != "" {
		return LinkRetryJob{}, false
	}

	now := time.Now().UTC()
	readyIndex := -1
	for index, job := range q.queued {
		if !job.NextAttemptAt.After(now) {
			readyIndex = index
			break
		}
	}
	if readyIndex < 0 {
		return LinkRetryJob{}, false
	}

	job := q.queued[readyIndex]
	q.queued = append(q.queued[:readyIndex], q.queued[readyIndex+1:]...)
	q.processingID = job.ID
	q.processing = &job
	q.saveLocked()
	return job, true
}

func (q *linkRetryQueue) finish(job LinkRetryJob, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.processingID = ""
	q.processing = nil
	defer q.saveLocked()

	switch {
	case err == nil, errors.Is(err, os.ErrNotExist):
		return
	default:
		job.Attempts += 1
		job.LastError = strings.TrimSpace(err.Error())
		if job.Attempts >= job.MaxAttempts {
			q.rejected = append(q.rejected, job)
			if overflow := len(q.rejected) - maxRejectedLinkRetries; overflow > 0 {
				q.rejected = slices.Delete(q.rejected, 0, overflow)
			}
			return
		}
		job.NextAttemptAt = time.Now().UTC().Add(q.interval)
		q.queued = append(q.queued, job)
	}
}
