package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, label string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", label)
}

func TestRegisterAndRunNow(t *testing.T) {
	s := New()
	var count atomic.Int64
	task := &Task{
		Type:     "test_task",
		Interval: time.Hour,
		Enabled:  true,
		Fn:       func(_ context.Context) { count.Add(1) },
	}
	s.Register(task)
	s.Start()
	defer s.Stop()

	if !s.RunNow("test_task") {
		t.Fatal("RunNow returned false for a registered enabled task")
	}
	waitFor(t, "task executed once", func() bool { return count.Load() == 1 })
}

func TestRunNowUnknownTaskReturnsFalse(t *testing.T) {
	s := New()
	s.Start()
	defer s.Stop()

	if s.RunNow("does_not_exist") {
		t.Error("expected false for unknown task")
	}
}

func TestRunNowDisabledTaskReturnsFalse(t *testing.T) {
	s := New()
	s.Register(&Task{
		Type:     "disabled_task",
		Interval: time.Hour,
		Enabled:  false,
		Fn:       func(_ context.Context) {},
	})
	s.Start()
	defer s.Stop()

	if s.RunNow("disabled_task") {
		t.Error("expected false for disabled task")
	}
}

func TestSetEnabledToggles(t *testing.T) {
	s := New()
	var count atomic.Int64
	s.Register(&Task{
		Type:     "tog",
		Interval: time.Hour,
		Enabled:  false,
		Fn:       func(_ context.Context) { count.Add(1) },
	})
	s.Start()
	defer s.Stop()

	if s.RunNow("tog") {
		t.Error("expected false before enabling")
	}
	s.SetEnabled("tog", true)
	if !s.RunNow("tog") {
		t.Fatal("RunNow returned false after enabling")
	}
	waitFor(t, "ran after enable", func() bool { return count.Load() == 1 })
	s.SetEnabled("tog", false)
	if s.RunNow("tog") {
		t.Error("expected false after disabling")
	}
}

func TestSetEnabledUnknownReturnsFalse(t *testing.T) {
	if New().SetEnabled("nope", true) {
		t.Error("expected false for unknown task")
	}
}

func TestRunNowDoesNotFireConcurrently(t *testing.T) {
	s := New()
	gate := make(chan struct{})
	s.Register(&Task{
		Type:     "slow",
		Interval: time.Hour,
		Enabled:  true,
		Fn:       func(_ context.Context) { <-gate },
	})
	s.Start()
	defer s.Stop()

	if !s.RunNow("slow") {
		t.Fatal("first RunNow failed")
	}
	time.Sleep(20 * time.Millisecond)
	if s.RunNow("slow") {
		t.Error("expected false while task already running")
	}
	close(gate)
}

func TestStatusReturnsRegisteredTask(t *testing.T) {
	s := New()
	s.Register(&Task{
		Type:     "st",
		Interval: 30 * time.Second,
		Enabled:  true,
		Fn:       func(_ context.Context) {},
	})
	s.Start()
	defer s.Stop()

	ss := s.Status()
	if len(ss) != 1 {
		t.Fatalf("expected 1 status entry, got %d", len(ss))
	}
	if ss[0].Type != "st" {
		t.Errorf("unexpected type: %q", ss[0].Type)
	}
	if !ss[0].Enabled {
		t.Error("expected Enabled=true")
	}
	if ss[0].IntervalSecs != 30 {
		t.Errorf("expected IntervalSecs=30, got %d", ss[0].IntervalSecs)
	}
}

func TestLastRunUpdatedAfterExecution(t *testing.T) {
	s := New()
	var count atomic.Int64
	s.Register(&Task{
		Type:     "lr",
		Interval: time.Hour,
		Enabled:  true,
		Fn:       func(_ context.Context) { count.Add(1) },
	})
	s.Start()
	defer s.Stop()

	before := time.Now()
	s.RunNow("lr")
	waitFor(t, "lr ran", func() bool { return count.Load() == 1 })

	ss := s.Status()
	if len(ss) == 0 || ss[0].LastRun == nil {
		t.Fatal("expected LastRun to be set after execution")
	}
	if ss[0].LastRun.Before(before) {
		t.Errorf("LastRun %v is before test start %v", ss[0].LastRun, before)
	}
}

func TestSetIntervalUnknownReturnsFalse(t *testing.T) {
	if New().SetInterval("nope", time.Minute) {
		t.Error("expected false for unknown task")
	}
}

func TestSetIntervalUpdates(t *testing.T) {
	s := New()
	s.Register(&Task{
		Type:     "iv",
		Interval: time.Hour,
		Enabled:  true,
		Fn:       func(_ context.Context) {},
	})
	if !s.SetInterval("iv", 5*time.Minute) {
		t.Error("SetInterval returned false for existing task")
	}
	s.mu.RLock()
	got := s.tasks["iv"].Interval
	s.mu.RUnlock()
	if got != 5*time.Minute {
		t.Errorf("expected 5m interval, got %v", got)
	}
}

func TestStopIsClean(t *testing.T) {
	s := New()
	s.Register(&Task{
		Type:     "stop",
		Interval: time.Hour,
		Enabled:  true,
		Fn:       func(_ context.Context) {},
	})
	s.Start()
	s.Stop()
}
