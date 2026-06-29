package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pjmj/bottle/internal/job"
)

func TestSubmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(job.Job{ID: "abc", Command: "echo hi", Status: job.StatusQueued})
	}))
	defer srv.Close()

	j, err := New(srv.URL).Submit(context.Background(), "echo hi")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if j.ID != "abc" || j.Status != job.StatusQueued {
		t.Errorf("got %+v", j)
	}
}

func TestGetNotFoundSurfacesServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "job not found"})
	}))
	defer srv.Close()

	_, err := New(srv.URL).Get(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The user-facing error should include both the status and the API message.
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "job not found") {
		t.Errorf("error = %q, want it to mention 404 and the message", err)
	}
}

func TestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]job.Job{
			{ID: "1", Command: "a", Status: job.StatusSucceeded},
			{ID: "2", Command: "b", Status: job.StatusFailed},
		})
	}))
	defer srv.Close()

	jobs, err := New(srv.URL).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("len = %d, want 2", len(jobs))
	}
}

func TestStreamLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit two data events, then an end event, in SSE format.
		fmt.Fprint(w, "data: hello\n\ndata: world\n\nevent: end\ndata: \n\n")
	}))
	defer srv.Close()

	var out strings.Builder
	if err := New(srv.URL).StreamLogs(context.Background(), "abc", &out); err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("output = %q, want it to contain hello and world", got)
	}
}
