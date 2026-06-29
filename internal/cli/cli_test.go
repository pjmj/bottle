package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pjmj/bottle/internal/job"
)

// runCmd drives the root command with the given args against a test server,
// returning whatever it printed to stdout. This exercises the full path: flag
// parsing -> command -> client -> HTTP.
func runCmd(t *testing.T, serverURL string, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--server", serverURL}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(%v): %v", args, err)
	}
	return out.String()
}

func TestSubmitCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(job.Job{ID: "xyz", Status: job.StatusQueued})
	}))
	defer srv.Close()

	out := runCmd(t, srv.URL, "submit", "echo", "hello")
	if !strings.Contains(out, "submitted job xyz") {
		t.Errorf("output = %q, want it to mention the submitted job", out)
	}
}

func TestListCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]job.Job{
			{ID: "1", Command: "echo a", Status: job.StatusSucceeded},
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv.URL, "list")
	// The header and the row should both appear.
	if !strings.Contains(out, "STATUS") || !strings.Contains(out, "succeeded") {
		t.Errorf("output = %q, want a table with the job", out)
	}
}
