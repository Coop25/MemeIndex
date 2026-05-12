package client

import (
	"context"
	"errors"
	"os"
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
}

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
	go q.run()
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
		return true
	}
	return false
}

func (q *linkRetryQueue) run() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		job, ok := q.dequeueReady()
		if !ok {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		err := q.processFn(ctx, job)
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
	return job, true
}

func (q *linkRetryQueue) finish(job LinkRetryJob, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.processingID = ""

	switch {
	case err == nil, errors.Is(err, os.ErrNotExist):
		return
	default:
		job.Attempts += 1
		job.LastError = strings.TrimSpace(err.Error())
		if job.Attempts >= job.MaxAttempts {
			q.rejected = append(q.rejected, job)
			return
		}
		job.NextAttemptAt = time.Now().UTC().Add(q.interval)
		q.queued = append(q.queued, job)
	}
}
