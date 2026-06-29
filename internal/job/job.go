// Package job defines the core domain type of the platform: a unit of work
// submitted by a user, plus the states it moves through during its lifetime.
// It has no dependencies on storage, HTTP, or any other layer — the domain
// sits at the center and everything else depends inward on it.
package job

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrEmptyCommand is returned by Validate when a job has no command to run.
var ErrEmptyCommand = errors.New("command must not be empty")

// Status is the lifecycle state of a job. A job starts queued, becomes running
// when the scheduler picks it up, then ends in exactly one terminal state:
// succeeded or failed.
//
//	queued ──► running ──► succeeded
//	                  └──► failed
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Job is a single unit of work. Pointer fields (ExitCode, StartedAt,
// FinishedAt) are nil until they become meaningful — a queued job has no exit
// code yet, so nil models "not set" more honestly than a zero value would.
type Job struct {
	ID         string     `json:"id"`
	Command    string     `json:"command"`
	Status     Status     `json:"status"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// New returns a freshly queued job with a generated ID and creation timestamp.
// Generating the ID here (rather than relying on a DB auto-increment) means the
// caller knows the ID immediately, and it stays unique even across multiple
// servers or databases.
func New(command string) *Job {
	return &Job{
		ID:        newID(),
		Command:   command,
		Status:    StatusQueued,
		CreatedAt: time.Now().UTC(),
	}
}

// Validate checks the job's invariants. It lives on the domain type so every
// entry point (HTTP, CLI, tests) enforces the same rule, rather than trusting
// each caller to remember. The HTTP layer calls this and maps a failure to a
// 400 Bad Request.
func (j *Job) Validate() error {
	if strings.TrimSpace(j.Command) == "" {
		return ErrEmptyCommand
	}
	return nil
}

// newID returns a random 16-character hex string (64 bits of entropy) — enough
// to avoid collisions for a project of this scale without pulling in a UUID
// dependency.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the OS entropy source is broken; there is
		// no sensible way to continue, so we panic rather than return a
		// non-unique or empty ID.
		panic(fmt.Sprintf("job: generate id: %v", err))
	}
	return hex.EncodeToString(b)
}
