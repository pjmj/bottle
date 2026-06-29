package logs

import (
	"io"
	"testing"
	"time"
)

func TestHistoryThenLive(t *testing.T) {
	b := New()
	w := b.Writer("j1")

	// Produce one line before anyone subscribes.
	_, _ = io.WriteString(w, "first\n")

	// A subscriber that connects now should see "first" as history.
	history, lines, cancel := b.Subscribe("j1")
	defer cancel()
	if len(history) != 1 || history[0] != "first" {
		t.Fatalf("history = %v, want [first]", history)
	}

	// A line produced after subscribing should arrive live.
	_, _ = io.WriteString(w, "second\n")
	select {
	case line := <-lines:
		if line != "second" {
			t.Errorf("live line = %q, want %q", line, "second")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live line")
	}

	// Closing the writer should close the subscriber's channel.
	_ = w.Close()
	select {
	case _, ok := <-lines:
		if ok {
			t.Error("expected channel to be closed after writer Close")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after writer Close")
	}
}

func TestSubscribeAfterClose(t *testing.T) {
	b := New()
	w := b.Writer("j2")
	_, _ = io.WriteString(w, "only line\n")
	_ = w.Close()

	// Subscribing to an already-finished job replays history and returns an
	// immediately-closed channel.
	history, lines, cancel := b.Subscribe("j2")
	defer cancel()
	if len(history) != 1 || history[0] != "only line" {
		t.Fatalf("history = %v, want [only line]", history)
	}
	if _, ok := <-lines; ok {
		t.Error("expected an already-closed channel for a finished job")
	}
}

func TestPartialLineFlushedOnClose(t *testing.T) {
	b := New()
	w := b.Writer("j3")

	// Output with no trailing newline must still be captured on Close.
	_, _ = io.WriteString(w, "no newline here")
	_ = w.Close()

	history, _, cancel := b.Subscribe("j3")
	defer cancel()
	if len(history) != 1 || history[0] != "no newline here" {
		t.Fatalf("history = %v, want [no newline here]", history)
	}
}

func TestMultipleLinesInOneWrite(t *testing.T) {
	b := New()
	w := b.Writer("j4")
	_, _ = io.WriteString(w, "a\nb\nc\n")
	_ = w.Close()

	history, _, cancel := b.Subscribe("j4")
	defer cancel()
	want := []string{"a", "b", "c"}
	if len(history) != len(want) {
		t.Fatalf("history = %v, want %v", history, want)
	}
	for i := range want {
		if history[i] != want[i] {
			t.Errorf("history[%d] = %q, want %q", i, history[i], want[i])
		}
	}
}
