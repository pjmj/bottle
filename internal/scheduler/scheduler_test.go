package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/logs"
	"github.com/pjmj/bottle/internal/store/sqlite"
)

// stubRunner is a Runner whose result is fixed by the test, so scheduler
// behavior can be verified deterministically without launching real processes.
type stubRunner struct {
	exitCode int
	err      error
	output   string
}

func (s stubRunner) Run(_ context.Context, _ string, logs io.Writer) (int, error) {
	if s.output != "" {
		_, _ = io.WriteString(logs, s.output)
	}
	return s.exitCode, s.err
}

func newSQLiteStore(t *testing.T) *sqlite.Store {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// runScheduled creates a job, runs it through a scheduler backed by the given
// runner, and returns the job's final persisted state.
func runScheduled(t *testing.T, st *sqlite.Store, r stubRunner) *job.Job {
	t.Helper()

	sch := New(st, r, logs.New(), discardLogger(), 2)
	ctx, cancel := context.WithCancel(context.Background())
	// Always stop the workers and wait for them to exit when the test ends.
	t.Cleanup(func() {
		cancel()
		sch.Wait()
	})
	sch.Start(ctx)

	j := job.New("some command")
	if err := st.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sch.Submit(ctx, j.ID); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	return waitForTerminal(t, st, j.ID)
}

// waitForTerminal polls the store until the job reaches a terminal state or the
// deadline passes. Polling is appropriate here because execution is async; the
// short interval keeps the test fast.
func waitForTerminal(t *testing.T, st *sqlite.Store, id string) *job.Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, err := st.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if j.Status == job.StatusSucceeded || j.Status == job.StatusFailed {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal state in time", id)
	return nil
}

func TestSchedulerSuccess(t *testing.T) {
	st := newSQLiteStore(t)
	got := runScheduled(t, st, stubRunner{exitCode: 0})

	if got.Status != job.StatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusSucceeded)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Error("expected StartedAt and FinishedAt to be set")
	}
}

func TestSchedulerNonZeroExitFails(t *testing.T) {
	st := newSQLiteStore(t)
	got := runScheduled(t, st, stubRunner{exitCode: 5})

	if got.Status != job.StatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusFailed)
	}
	if got.ExitCode == nil || *got.ExitCode != 5 {
		t.Errorf("ExitCode = %v, want 5", got.ExitCode)
	}
}

func TestSchedulerRunErrorFailsWithoutExitCode(t *testing.T) {
	st := newSQLiteStore(t)
	got := runScheduled(t, st, stubRunner{err: errors.New("could not start")})

	if got.Status != job.StatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusFailed)
	}
	// When the process never ran, there is no exit code to report.
	if got.ExitCode != nil {
		t.Errorf("ExitCode = %d, want nil", *got.ExitCode)
	}
}
