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
	"log/slog"
	"sync"
	"time"
)

// Scheduler manages recurring timers within the Hub server.
// A single root ticker fires every 1 minute and drives all registered
// recurring handlers based on their configured interval.
//
// All recurring handlers must be registered via RegisterRecurring before
// Start is called. RegisterRecurring is not safe for concurrent use.
type Scheduler struct {
	// Root ticker interval
	tickInterval time.Duration

	// Recurring handlers
	recurring []RecurringHandler

	// Tick counter (monotonically increasing)
	tickCount uint64

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// RecurringHandler defines a periodic task driven by the root ticker.
type RecurringHandler struct {
	Name     string                    // Human-readable name for logging
	Interval int                       // Run every N ticks (must be >= 1)
	Fn       func(ctx context.Context) // The work to perform
}

// NewScheduler creates a new Scheduler with a 1-minute root ticker interval.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tickInterval: 1 * time.Minute,
		stopCh:       make(chan struct{}),
	}
}

// RegisterRecurring registers a recurring handler that runs every intervalMinutes
// minutes. All handlers must be registered before Start is called.
//
// Tick-Zero Behavior: All recurring handlers run immediately on startup (tick 0)
// because 0 % N == 0 for any interval N. This is intentional.
func (s *Scheduler) RegisterRecurring(name string, intervalMinutes int, fn func(ctx context.Context)) {
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	s.recurring = append(s.recurring, RecurringHandler{
		Name:     name,
		Interval: intervalMinutes,
		Fn:       fn,
	})
}

// Start begins the root ticker loop and runs eligible handlers immediately
// on startup (tick 0). The provided context is used as the parent for handler
// invocations.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()

		// Run eligible handlers immediately on startup (tick 0).
		// All handlers fire at tick 0 because 0 % N == 0 for any interval.
		s.runRecurringHandlers(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.tickCount++
				s.runRecurringHandlers(ctx)
			}
		}
	}()
}

// Stop signals the scheduler to stop and waits for the root ticker goroutine
// to exit. In-flight handler goroutines are not tracked; they will be
// cancelled via the parent context when the server shuts down.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// runRecurringHandlers invokes all handlers whose interval divides the current
// tick count. Each handler runs in its own goroutine with a timeout context.
func (s *Scheduler) runRecurringHandlers(ctx context.Context) {
	for _, h := range s.recurring {
		if s.tickCount%uint64(h.Interval) == 0 {
			handler := h // capture loop variable
			go func() {
				handlerCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
				defer cancel()

				start := time.Now()
				slog.Debug("Scheduler: running recurring handler", "name", handler.Name, "tick", s.tickCount)

				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("Scheduler: recurring handler panicked",
								"name", handler.Name, "panic", r)
						}
					}()
					handler.Fn(handlerCtx)
				}()

				slog.Debug("Scheduler: recurring handler completed",
					"name", handler.Name, "duration", time.Since(start))
			}()
		}
	}
}
