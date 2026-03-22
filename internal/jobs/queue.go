package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

// ErrAlreadyActive is returned by Submit when a job of the same type is already
// queued or actively running.
var ErrAlreadyActive = errors.New("job of this type is already queued or running")

type workItem struct {
	jobType string
	fn      func(context.Context)
}

// Queue is a single-worker, priority-aware job queue with per-type deduplication.
// The priority lane (cap 5) is drained before the normal lane (cap 50).
// It is safe for concurrent use.
type Queue struct {
	priorityCh chan workItem
	normalCh   chan workItem

	active   map[string]int
	activeMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *zap.Logger

	submitted atomic.Int64
	completed atomic.Int64
}

// New constructs a Queue. If logger is nil, a no-op logger is used.
func New(logger *zap.Logger) *Queue {
	if logger == nil {
		logger = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		priorityCh: make(chan workItem, 5),
		normalCh:   make(chan workItem, 50),
		active:     make(map[string]int),
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
	}
}

// Start launches the background worker goroutine. Call once after New.
func (q *Queue) Start() {
	q.wg.Add(1)
	go q.worker()
}

// Stop signals the worker to exit after finishing any in-flight job and waits
// for it to complete. Pending items are discarded.
func (q *Queue) Stop() {
	q.cancel()
	q.wg.Wait()
}

// Submit enqueues fn for asynchronous execution under the given jobType key.
// If priority is true the item is placed in the high-priority lane. Returns
// ErrAlreadyActive without blocking if a job of this type is already queued
// or running.
func (q *Queue) Submit(jobType string, priority bool, fn func(context.Context)) error {
	q.activeMu.Lock()
	if q.active[jobType] > 0 {
		q.activeMu.Unlock()
		return fmt.Errorf("%w: %s", ErrAlreadyActive, jobType)
	}
	q.active[jobType]++
	q.activeMu.Unlock()

	item := workItem{jobType: jobType, fn: fn}
	q.submitted.Add(1)

	if priority {
		select {
		case q.priorityCh <- item:
			return nil
		default:
			// Priority lane full; fall through to normal lane.
		}
	}
	q.normalCh <- item
	return nil
}

// IsActive reports whether a job of the given type is currently queued or running.
func (q *Queue) IsActive(jobType string) bool {
	q.activeMu.Lock()
	defer q.activeMu.Unlock()
	return q.active[jobType] > 0
}

// Size returns the number of items sitting in the channels (not including any
// item actively executing).
func (q *Queue) Size() int {
	return len(q.priorityCh) + len(q.normalCh)
}

// Submitted returns the cumulative count of accepted submissions since New.
func (q *Queue) Submitted() int64 { return q.submitted.Load() }

// Completed returns the cumulative count of finished executions since New.
func (q *Queue) Completed() int64 { return q.completed.Load() }

// worker drains jobs sequentially, checking the priority lane first.
func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		// Drain the priority lane first without blocking.
		select {
		case item := <-q.priorityCh:
			q.run(item)
			continue
		default:
		}

		// Block until any lane has work or the queue is stopped.
		select {
		case <-q.ctx.Done():
			return
		case item := <-q.priorityCh:
			q.run(item)
		case item := <-q.normalCh:
			q.run(item)
		}
	}
}

func (q *Queue) run(item workItem) {
	defer func() {
		q.activeMu.Lock()
		q.active[item.jobType]--
		if q.active[item.jobType] <= 0 {
			delete(q.active, item.jobType)
		}
		q.activeMu.Unlock()
		q.completed.Add(1)
	}()

	q.logger.Info("queue job started", zap.String("type", item.jobType))
	item.fn(q.ctx)
	q.logger.Info("queue job finished", zap.String("type", item.jobType))
}
