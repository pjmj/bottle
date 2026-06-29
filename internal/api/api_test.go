package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/store"
)

// fakeStore is an in-memory store.Store used only in tests. Because the API
// depends on the interface, we can test every handler with this — no database,
// no disk, no cleanup, and tests run in microseconds. This is the concrete
// payoff of defining the Store interface in Step 1.
type fakeStore struct {
	jobs map[string]*job.Job
}

// Compile-time proof the fake honors the same contract as the real store.
var _ store.Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{jobs: make(map[string]*job.Job)}
}

func (f *fakeStore) Create(_ context.Context, j *job.Job) error {
	f.jobs[j.ID] = j
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*job.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return j, nil
}

func (f *fakeStore) List(_ context.Context) ([]*job.Job, error) {
	out := make([]*job.Job, 0, len(f.jobs))
	for _, j := range f.jobs {
		out = append(out, j)
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, j *job.Job) error {
	if _, ok := f.jobs[j.ID]; !ok {
		return store.ErrNotFound
	}
	f.jobs[j.ID] = j
	return nil
}

// recordingSubmitter is a no-op Submitter that remembers the IDs it was asked
// to schedule, so tests can assert the API enqueues created jobs.
type recordingSubmitter struct {
	ids []string
	err error // if set, Submit returns it
}

func (r *recordingSubmitter) Submit(_ context.Context, id string) error {
	if r.err != nil {
		return r.err
	}
	r.ids = append(r.ids, id)
	return nil
}

// fakeLogs is a no-op LogSubscriber that replays a fixed history and then ends
// (its channel is already closed). That's enough to test the SSE handler's
// happy path without a running job.
type fakeLogs struct {
	history []string
}

var _ LogSubscriber = fakeLogs{}

func (f fakeLogs) Subscribe(string) ([]string, <-chan string, func()) {
	ch := make(chan string)
	close(ch)
	return f.history, ch, func() {}
}

// newTestServer builds a Server backed by the fake store, a recording
// submitter, an empty log broker, and a logger that throws output away so tests
// stay quiet.
func newTestServer() (*Server, *recordingSubmitter) {
	sub := &recordingSubmitter{}
	srv := NewServer(newFakeStore(), sub, fakeLogs{}, discardLogger())
	return srv, sub
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCreateJob(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":"echo hello"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body)
	}

	var got job.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID == "" {
		t.Error("expected a generated ID")
	}
	if got.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", got.Command, "echo hello")
	}
	if got.Status != job.StatusQueued {
		t.Errorf("Status = %q, want %q", got.Status, job.StatusQueued)
	}
}

func TestCreateJobIsSubmitted(t *testing.T) {
	srv, sub := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":"echo hi"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var created job.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The handler must have enqueued exactly the job it created.
	if len(sub.ids) != 1 || sub.ids[0] != created.ID {
		t.Errorf("submitted ids = %v, want [%s]", sub.ids, created.ID)
	}
}

func TestCreateJobEmptyCommand(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":"  "}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateJobMalformedJSON(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateJobUnknownField(t *testing.T) {
	srv, _ := newTestServer()

	// DisallowUnknownFields should reject this misspelled field.
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"commnd":"oops"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetJobNotFound(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/jobs/nope", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateThenGet(t *testing.T) {
	srv, _ := newTestServer()

	// Create a job, then fetch it back by the ID the API returned.
	createReq := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":"sleep 1"}`))
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, createReq)

	var created job.Job
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getRec.Code, http.StatusOK)
	}
}

func TestJobLogsReplaysHistoryAndEnds(t *testing.T) {
	// Pre-create a job in the store so the existence check passes, then back
	// the server with a log broker that has two lines of history.
	fs := newFakeStore()
	j := job.New("echo hi")
	if err := fs.Create(context.Background(), j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	srv := NewServer(fs, &recordingSubmitter{},
		fakeLogs{history: []string{"line one", "line two"}}, discardLogger())

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+j.ID+"/logs", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"data: line one", "data: line two", "event: end"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got:\n%s", want, body)
		}
	}
}

func TestJobLogsNotFound(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/jobs/missing/logs", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListJobsReturnsEmptyArray(t *testing.T) {
	srv, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// With no jobs, the body must be `[]`, never `null`.
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}
