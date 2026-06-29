package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pjmj/bottle/internal/store"
)

// handleJobLogs streams a job's output to the client using Server-Sent Events
// (SSE). SSE is a good fit here: it's one-directional (server -> client),
// rides plain HTTP, and browsers reconnect automatically via EventSource —
// far less machinery than WebSockets for a "tail -f" use case.
func (s *Server) handleJobLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Confirm the job exists first, so an unknown ID is a clean 404 rather than
	// an empty stream that never ends.
	if _, err := s.store.Get(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("logs: get job", "id", id, "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not get job")
		return
	}

	// SSE response headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// This is the Step 0 WriteTimeout trap coming home: a streaming response
	// can stay open for minutes, but the server's global WriteTimeout would cut
	// it off. ResponseController lets us clear the write deadline for THIS
	// connection only, leaving the safe default in place everywhere else.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		// Not fatal: some ResponseWriters (e.g. httptest's) don't support it.
		s.logger.Warn("logs: clear write deadline", "id", id, "err", err)
	}

	history, lines, cancel := s.logs.Subscribe(id)
	defer cancel()

	// Replay everything produced before this client connected.
	for _, line := range history {
		if err := writeSSE(w, rc, "", line); err != nil {
			return // client went away
		}
	}

	// Then stream live until the job ends or the client disconnects.
	for {
		select {
		case <-r.Context().Done():
			// Client closed the connection; cancel (deferred) detaches us.
			return
		case line, ok := <-lines:
			if !ok {
				// Channel closed: the job finished. Emit a terminal event so
				// the client knows to stop, rather than guessing from silence.
				_ = writeSSE(w, rc, "end", "")
				return
			}
			if err := writeSSE(w, rc, "", line); err != nil {
				return
			}
		}
	}
}

// writeSSE writes one Server-Sent Event and flushes it immediately. Flushing is
// essential: without it the response sits in a buffer and the client sees
// nothing until the handler returns, defeating live streaming.
//
// An optional event name produces an "event:" line; data goes on a "data:"
// line; the blank line terminates the event per the SSE wire format.
func writeSSE(w http.ResponseWriter, rc *http.ResponseController, event, data string) error {
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return rc.Flush()
}
