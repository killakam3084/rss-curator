package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// TaskStatus is the public snapshot of a scheduled task returned by the API.
type TaskStatus struct {
	Type         string     `json:"type"`
	IntervalSecs int64      `json:"interval_secs"`
	Enabled      bool       `json:"enabled"`
	Running      bool       `json:"running"`
	LastRun      *time.Time `json:"last_run,omitempty"`
	NextRun      time.Time  `json:"next_run"`
}

// Task describes a periodic background operation. Fn is called on each
// scheduled tick (and on-demand via RunNow).
type Task struct {
	Type     string
	Interval time.Duration
	Enabled  bool
	Fn       func(context.Context)

	running atomic.Bool
	mu      sync.Mutex
	lastRun *time.Time
	nextRun time.Time
}

// Scheduler manages a set of registered periodic tasks, each in its own
// goroutine. If a task is still executing when its next tick fires, the
// second invocation is silently skipped.
type Scheduler struct {
	tasks  map[string]*Task
	mu     sync.RWMutex
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// New returns a ready-to-use Scheduler. Call Start to begin dispatching tasks.
func New() *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		tasks:  make(map[string]*Task),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Register adds a task. Must be called before Start.
func (s *Scheduler) Register(t *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.Type] = t
}

// Start launches a per-task goroutine for every registered task.
func (s *Scheduler) Start() {
	s.mu.RLock()
	tasks := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	s.mu.RUnlock()
	for _, t := range tasks {
		s.wg.Add(1)
		go s.taskLoop(t)
	}
}

// Stop signals all task goroutines to exit and waits up to 30 seconds for
// them to finish. Tasks that have not returned after the deadline are not
// force-killed but the function returns so shutdown can proceed.
func (s *Scheduler) Stop() {
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

// RunNow dispatches the named task immediately. Returns false if not found,
// disabled, or already running.
func (s *Scheduler) RunNow(taskType string) bool {
	s.mu.RLock()
	t, ok := s.tasks[taskType]
	s.mu.RUnlock()
	if !ok || !t.Enabled {
		return false
	}
	return s.fire(t)
}

// SetEnabled toggles a task enabled flag at runtime.
func (s *Scheduler) SetEnabled(taskType string, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskType]
	if !ok {
		return false
	}
	t.Enabled = enabled
	return true
}

// SetInterval updates a task interval at runtime. Takes effect on the next tick.
func (s *Scheduler) SetInterval(taskType string, interval time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskType]
	if !ok {
		return false
	}
	t.Interval = interval
	return true
}

// Status returns a snapshot of every registered task.
func (s *Scheduler) Status() []TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TaskStatus, 0, len(s.tasks))
	for _, t := range s.tasks {
		t.mu.Lock()
		lastRun := t.lastRun
		nextRun := t.nextRun
		t.mu.Unlock()
		out = append(out, TaskStatus{
			Type:         t.Type,
			IntervalSecs: int64(t.Interval.Seconds()),
			Enabled:      t.Enabled,
			Running:      t.running.Load(),
			LastRun:      lastRun,
			NextRun:      nextRun,
		})
	}
	return out
}

func (s *Scheduler) taskLoop(t *Task) {
	defer s.wg.Done()
	t.mu.Lock()
	t.nextRun = time.Now().Add(t.Interval)
	t.mu.Unlock()
	currentInterval := t.Interval
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case tick := <-ticker.C:
			t.mu.Lock()
			newInterval := t.Interval
			t.mu.Unlock()
			if newInterval != currentInterval && newInterval > 0 {
				ticker.Reset(newInterval)
				currentInterval = newInterval
			}
			t.mu.Lock()
			t.nextRun = tick.Add(currentInterval)
			t.mu.Unlock()
			if !t.Enabled {
				continue
			}
			s.fire(t)
		}
	}
}

func (s *Scheduler) fire(t *Task) bool {
	if !t.running.CompareAndSwap(false, true) {
		return false
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer t.running.Store(false)
		now := time.Now()
		t.mu.Lock()
		t.lastRun = &now
		t.mu.Unlock()
		t.Fn(s.ctx)
	}()
	return true
}
