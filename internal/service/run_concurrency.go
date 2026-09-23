package service

import "context"

// This file implements RunService's concurrency scheduler: a counting
// semaphore over a FIFO token channel bounding how many runs execute at
// once, plus the "queued" store transitions that make a waiting run visible
// - see run-concurrency-and-retention design.md D1/D2. Split out of
// service.go, which otherwise mixes it with ProjectService, WebhookService,
// and the credential/URL-validation helpers - see REVIEW.md R14.

// SetMaxConcurrentRuns bounds how many runs may execute at once. A
// non-positive n restores unlimited concurrency. Call it once during startup
// wiring, before serving traffic.
func (s *RunService) SetMaxConcurrentRuns(n int) {
	if n <= 0 {
		s.slots = nil
		return
	}
	slots := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		slots <- struct{}{}
	}
	s.slots = slots
}

// MaxConcurrentRuns reports the concurrency limit set by SetMaxConcurrentRuns,
// or 0 for unlimited. It exists so callers outside this package - notably
// startup wiring tests - can verify SetMaxConcurrentRuns was actually called
// with the configured value; see REVIEW.md R7.
func (s *RunService) MaxConcurrentRuns() int {
	return cap(s.slots)
}

// tryAcquireSlot claims a slot without blocking, reporting whether one was
// free. A nil slots channel (unlimited concurrency) always succeeds.
func (s *RunService) tryAcquireSlot() bool {
	if s.slots == nil {
		return true
	}
	select {
	case <-s.slots:
		return true
	default:
		return false
	}
}

// waitForSlot blocks until a slot frees or ctx is done, reporting which
// happened first. A nil slots channel (unlimited concurrency) always
// succeeds immediately.
//
// When a slot frees at the same instant ctx is canceled, Go's select
// resolves the tie non-deterministically and could pick the slot-ready case
// even though the caller must not proceed - see REVIEW.md R2. The re-check
// after receiving is race-free: once a context's CancelFunc has returned,
// ctx.Err() is permanently non-nil, so this catches every case where
// cancellation happened-before or concurrently with the receive, regardless
// of which select branch fired. A cancellation that lands microseconds
// later still races the caller's subsequent work (StartQueuedRun,
// executeRun) the same way it already does for a run cancelled just after
// it starts executing - an accepted, pre-existing risk this does not widen.
func (s *RunService) waitForSlot(ctx context.Context) bool {
	if s.slots == nil {
		return true
	}
	select {
	case <-s.slots:
		if ctx.Err() != nil {
			s.releaseSlot()
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseSlot returns a slot to the pool, unblocking the next FIFO waiter.
// A no-op under unlimited concurrency.
func (s *RunService) releaseSlot() {
	if s.slots == nil {
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		// Never blocks in practice: releaseSlot is called at most once per
		// acquired slot, so the channel can never be over capacity.
	}
}

// markQueuedIfBusy tries to claim a slot without blocking. If none is free,
// it records runID as "queued" in the store - see design.md D1/D2. It
// reports whether a slot was claimed immediately, so the caller knows
// whether it still owns one (nothing further to wait for) or must go on to
// waitAndStart.
func (s *RunService) markQueuedIfBusy(runID int) (acquired bool) {
	if s.tryAcquireSlot() {
		return true
	}
	if s.runStore != nil {
		_ = s.runStore.SetRunQueued(runID)
	}
	return false
}

// waitAndStart blocks until a slot frees or ctx is done, then flips runID
// from "queued" to "running" in the store. It returns false if ctx is done
// first, or if the run was already canceled by the time the slot was
// claimed - in either case any slot this call claimed has already been
// released back, and the caller must not execute the run.
func (s *RunService) waitAndStart(ctx context.Context, runID int) bool {
	if !s.waitForSlot(ctx) {
		return false
	}
	if s.runStore != nil {
		ok, _ := s.runStore.StartQueuedRun(runID)
		if !ok {
			s.releaseSlot()
			return false
		}
	}
	return true
}

// acquireSlot blocks the caller until a concurrency slot is free or ctx is
// done, marking runID "queued" in the store meanwhile and "running" once
// acquired - see design.md D1/D2. Used by RunTask and ApproveManualTask,
// which execute synchronously in the caller's goroutine, so there is no
// separate phase to split the decision across; Start (asynchronous) uses
// markQueuedIfBusy/waitAndStart directly instead - see REVIEW.md R10. It
// returns false only when ctx is done before a slot frees, or the run was
// canceled while queued (in which case the caller must not execute it).
func (s *RunService) acquireSlot(ctx context.Context, runID int) bool {
	if s.markQueuedIfBusy(runID) {
		return true
	}
	return s.waitAndStart(ctx, runID)
}

// Counts reports the current running and queued run counts for the
// /metrics gauges - see observability design.md D3: derived on scrape from
// the scheduler's own live state, never a separately tracked counter that
// could drift. running comes from the concurrency semaphore itself
// (occupied slots = capacity - free tokens currently sitting in the
// channel); a nil slots channel (unlimited concurrency) reports 0, since
// there is no capacity to measure occupancy against. queued has no
// in-memory counterpart - "queued" is purely a store status, so it is read
// from RunStore.CountRunsByStatus, the same source the dashboard already
// uses for the same number.
func (s *RunService) Counts() (running, queued int) {
	if s.slots != nil {
		running = cap(s.slots) - len(s.slots)
	}
	if s.runStore != nil {
		if counts, err := s.runStore.CountRunsByStatus(); err == nil {
			queued = counts["queued"]
		}
	}
	return running, queued
}
