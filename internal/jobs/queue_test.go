package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitCompleted(t *testing.T, q *Queue, n int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if q.Completed() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d completed jobs (got %d)", n, q.Completed())
}

func TestSubmitAndExecute(t *testing.T) {
	q := New(nil)
	q.Start()
	defer q.Stop()

	var ran bool
	var mu sync.Mutex
	err := q.Submit("feed_check", false, func(_ context.Context) {
		mu.Lock()
		ran = true
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	waitCompleted(t, q, 1)

	mu.Lock()
	defer mu.Unlock()
	if !ran {
		t.Error("expected job function to have run")
	}
}

func TestSubmitDuplicateReturnsErrAlreadyActive(t *testing.T) {
	q := New(nil)
	q.Start()
	defer q.Stop()

	gate := make(chan struct{})
	_ = q.Submit("long_job", false, func(_ context.Context) {
		<-gate
	})
	time.Sleep(20 * time.Millisecond)

	err := q.Submit("long_job", false, func(_ context.Context) {})
	if !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("expected ErrAlreadyActive, got %v", err)
	}
	close(gate)
}

func TestIsActiveReflectsRunningJob(t *testing.T) {
	q := New(nil)
	q.Start()
	defer q.Stop()

	gate := make(chan struct{})
	_ = q.Submit("rescore", false, func(_ context.Context) { <-gate })
	time.Sleep(20 * time.Millisecond)

	if !q.IsActive("rescore") {
		t.Error("expected IsActive to be true while job is running")
	}
	close(gate)

	waitCompleted(t, q, 1)
	if q.IsActive("rescore") {
		t.Error("expected IsActive to be false after job completes")
	}
}

func TestIsActiveUnknownTypeIsFalse(t *testing.T) {
	q := New(nil)
	if q.IsActive("nonexistent") {
		t.Error("expected IsActive false for unknown type")
	}
}

func TestPriorityLaneDrainedFirst(t *testing.T) {
	q := New(nil)

	var order []string
	var mu sync.Mutex
	record := func(name string) func(context.Context) {
		return func(_ context.Context) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}
	}

	gate := make(chan struct{})
	_ = q.Submit("blocker", false, func(_ context.Context) { <-gate })
	q.Start()
	time.Sleep(20 * time.Millisecond)

	_ = q.Submit("normal", false, record("normal"))
	_ = q.Submit("priority", true, record("priority"))
	close(gate)

	waitCompleted(t, q, 3)

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 || order[0] != "priority" {
		t.Errorf("expected priority job first, got order: %v", order)
	}
}

func TestSubmittedAndCompletedCounters(t *testing.T) {
	q := New(nil)
	q.Start()
	defer q.Stop()

	for i := 0; i < 5; i++ {
		jobType := "job_" + string(rune('a'+i))
		_ = q.Submit(jobType, false, func(_ context.Context) {})
	}

	waitCompleted(t, q, 5)

	if q.Submitted() != 5 {
		t.Errorf("expected Submitted()=5, got %d", q.Submitted())
	}
	if q.Completed() != 5 {
		t.Errorf("expected Completed()=5, got %d", q.Completed())
	}
}

func TestSizeDecreasesAsJobsRun(t *testing.T) {
	q := New(nil)
	for i := 0; i < 3; i++ {
		jobType := "sz_job_" + string(rune('a'+i))
		_ = q.Submit(jobType, false, func(_ context.Context) {})
	}
	before := q.Size()
	if before != 3 {
		t.Fatalf("expected Size()=3 before start, got %d", before)
	}

	q.Start()
	waitCompleted(t, q, 3)

	if q.Size() != 0 {
		t.Errorf("expected Size()=0 after all jobs run, got %d", q.Size())
	}
	q.Stop()
}

func TestStopDrainsInFlightJob(t *testing.T) {
	q := New(nil)
	q.Start()

	started := make(chan struct{})
	done := make(chan struct{})
	_ = q.Submit("stopper", false, func(_ context.Context) {
		close(started)
		time.Sleep(50 * time.Millisecond)
		close(done)
	})

	<-started // wait until the job is actually executing
	q.Stop()  // must not return until the in-flight job finishes
	select {
	case <-done:
	default:
		t.Error("Stop() returned before the in-flight job finished")
	}
}

func TestContextCancelledWhenQueueStopped(t *testing.T) {
	q := New(nil)
	q.Start()

	ctxDone := make(chan struct{})
	_ = q.Submit("ctx_job", false, func(ctx context.Context) {
		<-ctx.Done()
		close(ctxDone)
	})
	time.Sleep(20 * time.Millisecond)
	q.Stop()

	select {
	case <-ctxDone:
	case <-time.After(time.Second):
		t.Error("job context was not cancelled after Stop()")
	}
}
