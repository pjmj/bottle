// Package scheduler runs queued jobs concurrently. It pulls job IDs off an
// internal queue with a fixed pool of worker goroutines, executes each through
// a runner.Runner, and records the resulting state transitions in the store.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/logs"
	"github.com/pjmj/bottle/internal/runner"
	"github.com/pjmj/bottle/internal/store"
)

// queueBuffer bounds how many submitted-but-not-yet-started jobs we hold in
// memory. A bounded channel provides natural backpressure: if the system is
// saturated, Submit blocks rather than letting an unbounded queue grow until
// it exhausts memory.
const queueBuffer = 1024

// dbTimeout caps each individual store operation the scheduler performs.
const dbTimeout = 5 * time.Second

// Scheduler owns a pool of workers and the queue that feeds them.
//
// It depends on the concrete *logs.Broker rather than an interface: there is
// one implementation, and the broker is already simple and deterministic in
// tests, so an interface here would be ceremony with no payoff. (Contrast the
// API, which interfaces the broker because faking the streaming side genuinely
// simplifies handler tests — interface at the boundary where it pays.)
type Scheduler struct {
	store   store.Store
	runner  runner.Runner
	logs    *logs.Broker
	logger  *slog.Logger
	queue   chan string
	workers int
	wg      sync.WaitGroup
}

// New constructs a Scheduler. workers is the number of jobs that may run
// concurrently.
func New(st store.Store, r runner.Runner, lb *logs.Broker, logger *slog.Logger, workers int) *Scheduler {
	if workers < 1 {
		workers = 1
	}
	return &Scheduler{
		store:   st,
		runner:  r,
		logs:    lb,
		logger:  logger,
		queue:   make(chan string, queueBuffer),
		workers: workers,
	}
}

// Start launches the worker goroutines. They run until ctx is cancelled.
// Cancelling ctx also kills any job currently executing (the run context is
// derived from it), which is what we want on server shutdown.
func (s *Scheduler) Start(ctx context.Context) {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.worker(ctx)
		}()
	}
	s.logger.Info("scheduler started", "workers", s.workers)
}

// Wait blocks until all workers have exited. Call it after cancelling the
// context passed to Start to drain the pool during graceful shutdown.
func (s *Scheduler) Wait() { s.wg.Wait() }

// Submit enqueues a job ID for execution. It blocks if the queue is full
// (backpressure) and returns ctx.Err() if the caller's context is cancelled
// first, so a slow scheduler can never wedge an HTTP request indefinitely.
func (s *Scheduler) Submit(ctx context.Context, id string) error {
	select {
	case s.queue <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// worker is the loop each goroutine runs: take the next job ID, process it,
// repeat — until the context is cancelled.
func (s *Scheduler) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.queue:
			s.process(ctx, id)
		}
	}
}

// process drives one job through its lifecycle.
//
// Important context split: the job's *execution* uses runCtx (so it is killed
// on shutdown), but every *store write* uses a fresh, detached context. That
// way we can always record the final state — even while the server is shutting
// down and runCtx is already cancelled. If we wrote with the cancelled context,
// the job would be left stuck in "running".
func (s *Scheduler) process(runCtx context.Context, id string) {
	j, err := s.load(id)
	if err != nil {
		s.logger.Error("scheduler: load job", "id", id, "err", err)
		return
	}

	// queued -> running
	startedAt := time.Now().UTC()
	j.Status = job.StatusRunning
	j.StartedAt = &startedAt
	if err := s.save(j); err != nil {
		s.logger.Error("scheduler: mark running", "id", id, "err", err)
		return
	}

	// Execute, streaming the command's output into the job's log stream. The
	// same writer is given to both stdout and stderr inside the runner, so
	// os/exec serializes writes to it.
	w := s.logs.Writer(j.ID)
	exitCode, runErr := s.runner.Run(runCtx, j.Command, w)
	// Close flushes any trailing partial line and ends the stream so live
	// subscribers know the job is done — even if the run failed.
	if cerr := w.Close(); cerr != nil {
		s.logger.Error("scheduler: close log writer", "id", id, "err", cerr)
	}

	// running -> succeeded/failed
	finishedAt := time.Now().UTC()
	j.FinishedAt = &finishedAt
	switch {
	case runErr != nil:
		// The process never ran (or was killed). No meaningful exit code, so
		// ExitCode stays nil.
		s.logger.Error("scheduler: run job", "id", id, "err", runErr)
		j.Status = job.StatusFailed
	case exitCode == 0:
		j.Status = job.StatusSucceeded
		j.ExitCode = &exitCode
	default:
		j.Status = job.StatusFailed
		j.ExitCode = &exitCode
	}

	if err := s.save(j); err != nil {
		s.logger.Error("scheduler: mark finished", "id", id, "err", err)
	}
}

// load and save wrap store access in a fresh, bounded context so a single slow
// or shutdown-cancelled operation cannot hang a worker or lose a state update.
func (s *Scheduler) load(id string) (*job.Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	return s.store.Get(ctx, id)
}

func (s *Scheduler) save(j *job.Job) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	return s.store.Update(ctx, j)
}
