package runner

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalRunnerSuccess(t *testing.T) {
	r := NewLocalRunner()

	var out strings.Builder
	code, err := r.Run(context.Background(), "echo hello", &out)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("output = %q, want it to contain %q", out.String(), "hello")
	}
}

func TestLocalRunnerNonZeroExit(t *testing.T) {
	r := NewLocalRunner()

	// A non-zero exit is a job outcome, not a runner error: err must be nil
	// and the code must be reported faithfully.
	code, err := r.Run(context.Background(), "exit 3", io.Discard)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestLocalRunnerContextCancelled(t *testing.T) {
	r := NewLocalRunner()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	_, err := r.Run(ctx, "echo hi", io.Discard)
	if err == nil {
		t.Fatal("Run: expected an error from a cancelled context, got nil")
	}
}
