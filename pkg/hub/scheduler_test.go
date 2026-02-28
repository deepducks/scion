// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestScheduler creates a scheduler with a fast tick interval for testing.
func newTestScheduler(interval time.Duration) *Scheduler {
	s := NewScheduler()
	s.tickInterval = interval
	return s
}

func TestSchedulerStartStop(t *testing.T) {
	s := newTestScheduler(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Give it time to run a few ticks
	time.Sleep(120 * time.Millisecond)

	s.Stop()

	// Verify stop is idempotent-safe (wg.Wait returns immediately)
	// If Stop didn't properly signal, this would deadlock.
}

func TestSchedulerTickZero(t *testing.T) {
	s := newTestScheduler(1 * time.Second) // long interval — we only care about tick 0

	var called atomic.Int32

	s.RegisterRecurring("tick-zero-handler", 1, func(ctx context.Context) {
		called.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Wait for tick-0 handler to execute
	deadline := time.After(500 * time.Millisecond)
	for {
		if called.Load() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("tick-zero handler was not invoked within timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	s.Stop()

	if got := called.Load(); got != 1 {
		t.Errorf("expected tick-zero handler to be called once, got %d", got)
	}
}

func TestSchedulerRecurringInterval(t *testing.T) {
	s := newTestScheduler(30 * time.Millisecond)

	var every1 atomic.Int32
	var every2 atomic.Int32
	var every3 atomic.Int32

	s.RegisterRecurring("every-1", 1, func(ctx context.Context) {
		every1.Add(1)
	})
	s.RegisterRecurring("every-2", 2, func(ctx context.Context) {
		every2.Add(1)
	})
	s.RegisterRecurring("every-3", 3, func(ctx context.Context) {
		every3.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Let 6 ticks pass (tick 0..6 = 7 invocations for every-1)
	// tick 0: all fire. tick 1: every-1. tick 2: every-1, every-2. tick 3: every-1, every-3.
	// tick 4: every-1, every-2. tick 5: every-1. tick 6: every-1, every-2, every-3.
	time.Sleep(220 * time.Millisecond) // ~7 ticks at 30ms

	s.Stop()

	got1 := every1.Load()
	got2 := every2.Load()
	got3 := every3.Load()

	// every-1 should run on every tick (7 times for ticks 0-6)
	if got1 < 5 {
		t.Errorf("every-1 handler expected at least 5 invocations, got %d", got1)
	}
	// every-2 should run on ticks 0, 2, 4, 6 (4 times)
	if got2 < 3 {
		t.Errorf("every-2 handler expected at least 3 invocations, got %d", got2)
	}
	// every-3 should run on ticks 0, 3, 6 (3 times)
	if got3 < 2 {
		t.Errorf("every-3 handler expected at least 2 invocations, got %d", got3)
	}
	// every-1 should always run more than every-2, which runs more than every-3
	if got1 <= got2 {
		t.Errorf("every-1 (%d) should have more invocations than every-2 (%d)", got1, got2)
	}
	if got2 <= got3 {
		t.Errorf("every-2 (%d) should have more invocations than every-3 (%d)", got2, got3)
	}
}

func TestSchedulerHandlerPanicRecovery(t *testing.T) {
	s := newTestScheduler(50 * time.Millisecond)

	var panickerCalled atomic.Int32
	var normalCalled atomic.Int32

	s.RegisterRecurring("panicker", 1, func(ctx context.Context) {
		panickerCalled.Add(1)
		panic("test panic")
	})
	s.RegisterRecurring("normal", 1, func(ctx context.Context) {
		normalCalled.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Wait for at least 2 ticks
	time.Sleep(130 * time.Millisecond)

	s.Stop()

	if got := panickerCalled.Load(); got < 2 {
		t.Errorf("panicking handler should have been called at least 2 times, got %d", got)
	}
	if got := normalCalled.Load(); got < 2 {
		t.Errorf("normal handler should have been called at least 2 times despite panic in other handler, got %d", got)
	}
}

func TestSchedulerContextCancellation(t *testing.T) {
	s := newTestScheduler(50 * time.Millisecond)

	var called atomic.Int32

	s.RegisterRecurring("counter", 1, func(ctx context.Context) {
		called.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Let tick 0 fire
	time.Sleep(30 * time.Millisecond)

	// Cancel context — scheduler should stop
	cancel()

	// Wait for scheduler to observe cancellation
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good — Stop returned
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
}

func TestSchedulerHandlerReceivesContext(t *testing.T) {
	s := newTestScheduler(1 * time.Second)

	var mu sync.Mutex
	var handlerCtx context.Context

	s.RegisterRecurring("ctx-check", 1, func(ctx context.Context) {
		mu.Lock()
		handlerCtx = ctx
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)

	// Wait for tick 0
	deadline := time.After(500 * time.Millisecond)
	for {
		mu.Lock()
		got := handlerCtx
		mu.Unlock()
		if got != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("handler was not invoked within timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	s.Stop()

	mu.Lock()
	defer mu.Unlock()

	// The handler context should have a deadline (55-second timeout)
	if _, ok := handlerCtx.Deadline(); !ok {
		t.Error("handler context should have a deadline from the 55-second timeout")
	}
}

func TestSchedulerMinimumInterval(t *testing.T) {
	s := newTestScheduler(30 * time.Millisecond)

	var called atomic.Int32

	// Register with invalid interval (0) — should be clamped to 1
	s.RegisterRecurring("clamped", 0, func(ctx context.Context) {
		called.Add(1)
	})

	if s.recurring[0].Interval != 1 {
		t.Errorf("expected interval to be clamped to 1, got %d", s.recurring[0].Interval)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	s.Stop()

	if got := called.Load(); got < 2 {
		t.Errorf("clamped handler should have been called at least 2 times, got %d", got)
	}
}

func TestSchedulerNoHandlers(t *testing.T) {
	s := newTestScheduler(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with no handlers — should not panic
	s.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	s.Stop()
}
