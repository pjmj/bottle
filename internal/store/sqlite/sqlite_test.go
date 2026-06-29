package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/store"
)

// newTestStore creates a Store backed by a throwaway database file inside the
// test's temp directory. t.TempDir() is cleaned up automatically when the test
// ends, so each test runs against a fresh, isolated database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	j := job.New("echo hello")
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", got.Command, "echo hello")
	}
	if got.Status != job.StatusQueued {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusQueued)
	}
	if got.ExitCode != nil {
		t.Errorf("ExitCode = %d, want nil", *got.ExitCode)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Error("a queued job should have nil StartedAt and FinishedAt")
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

func TestUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	j := job.New("sleep 1")
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate the job running to completion.
	now := time.Now().UTC()
	code := 0
	j.Status = job.StatusSucceeded
	j.StartedAt = &now
	j.FinishedAt = &now
	j.ExitCode = &code
	if err := s.Update(ctx, j); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != job.StatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusSucceeded)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Error("expected StartedAt and FinishedAt to be set after update")
	}
}

func TestUpdateNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.Update(context.Background(), job.New("noop"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create three jobs with distinct creation times so ordering is testable.
	for i, cmd := range []string{"first", "second", "third"} {
		j := job.New(cmd)
		j.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		if err := s.Create(ctx, j); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	jobs, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(jobs))
	}
	// ORDER BY created_at DESC means the most recent ("third") comes first.
	if jobs[0].Command != "third" {
		t.Errorf("jobs[0].Command = %q, want %q", jobs[0].Command, "third")
	}
}
