// Package store defines the persistence boundary for jobs. It declares *what*
// operations the rest of the application needs (the Store interface) without
// saying *how* they are implemented. Concrete implementations (e.g. SQLite)
// live in subpackages and depend on this package, not the other way around.
package store

import (
	"context"
	"errors"

	"github.com/pjmj/bottle/internal/job"
)

// ErrNotFound is returned by Get and Update when no job with the given ID
// exists. Callers compare against it with errors.Is, so they can map a
// missing job to an HTTP 404 without knowing anything about the database.
var ErrNotFound = errors.New("job not found")

// Store is the contract for persisting and retrieving jobs. Everything that
// needs to read or write jobs (the HTTP API, the scheduler) depends only on
// this interface — never on a concrete database. That lets us swap SQLite for
// Postgres, or substitute an in-memory fake in tests, with zero changes to
// callers.
//
// Every method takes a context.Context as its first argument so that database
// calls inherit request deadlines and cancellation.
type Store interface {
	// Create persists a new job. It is an error to create a job whose ID
	// already exists.
	Create(ctx context.Context, j *job.Job) error

	// Get returns the job with the given ID, or ErrNotFound if none exists.
	Get(ctx context.Context, id string) (*job.Job, error)

	// List returns all jobs, newest first.
	List(ctx context.Context) ([]*job.Job, error)

	// Update overwrites the mutable fields of an existing job (status, exit
	// code, timestamps). It returns ErrNotFound if the job does not exist.
	Update(ctx context.Context, j *job.Job) error
}
