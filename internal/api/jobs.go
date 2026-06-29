package api

import (
	"errors"
	"net/http"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/store"
)

// createJobRequest is the expected body of POST /jobs. Defining a dedicated
// request type (rather than decoding straight into job.Job) means clients can
// only set the fields we allow — they can't smuggle in a status or a fake
// timestamp. This separation of "wire shape" from "domain type" is good API
// hygiene.
type createJobRequest struct {
	Command string `json:"command"`
}

// handleCreateJob handles POST /jobs.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	req, err := decode[createJobRequest](r)
	if err != nil {
		// A body we can't parse is the client's fault: 400, not 500.
		s.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	j := job.New(req.Command)
	if err := j.Validate(); err != nil {
		// Domain validation failed (e.g. empty command) — surface the reason.
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.Create(r.Context(), j); err != nil {
		// A storage failure is the server's fault: log the detail, but don't
		// leak internals to the client.
		s.logger.Error("create job", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not create job")
		return
	}

	// Hand the job to the scheduler to run asynchronously. The job is already
	// persisted as "queued", so even if this fails the resource exists; we
	// report a 500 so the client knows it will not run without intervention.
	if err := s.scheduler.Submit(r.Context(), j.ID); err != nil {
		s.logger.Error("submit job", "id", j.ID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not schedule job")
		return
	}

	// 201 Created is the correct status for "a new resource now exists". The
	// body reflects its current state: queued. (202 Accepted would also be
	// defensible since execution is async, but we did create a fetchable
	// resource, so 201 with the representation is the choice here.)
	s.writeJSON(w, http.StatusCreated, j)
}

// handleListJobs handles GET /jobs.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.List(r.Context())
	if err != nil {
		s.logger.Error("list jobs", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not list jobs")
		return
	}

	// A nil slice encodes to JSON `null`, which is awkward for clients. Coerce
	// it to an empty slice so the response is always a JSON array `[]`.
	if jobs == nil {
		jobs = []*job.Job{}
	}
	s.writeJSON(w, http.StatusOK, jobs)
}

// handleGetJob handles GET /jobs/{id}.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	// PathValue reads the {id} wildcard from the route pattern (Go 1.22+).
	id := r.PathValue("id")

	j, err := s.store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		// This is the 404 path we traced in the Step 1 questions: the store
		// reports a domain "not found", and only here does it become HTTP 404.
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		s.logger.Error("get job", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not get job")
		return
	}

	s.writeJSON(w, http.StatusOK, j)
}
