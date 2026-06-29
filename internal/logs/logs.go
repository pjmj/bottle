// Package logs is an in-memory pub/sub broker for job output. For each job it
// accumulates the lines produced so far (so a client connecting late still sees
// the whole history) and fans new lines out to any number of live subscribers
// (so a client watching gets them in real time).
//
// Logs here are ephemeral: they live for the lifetime of the process. A
// production platform would also persist them — typically streaming live to the
// client while archiving to object storage (e.g. S3) for later retrieval. The
// broker's Writer is an io.WriteCloser, so swapping in a tee to durable storage
// is a localized change.
package logs

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

// subscriberBuffer is the per-subscriber channel capacity. It absorbs short
// bursts so a briefly-slow client doesn't lose lines.
const subscriberBuffer = 1024

// Broker holds one stream per job ID.
type Broker struct {
	mu      sync.Mutex
	streams map[string]*stream
}

// New returns an empty Broker.
func New() *Broker {
	return &Broker{streams: make(map[string]*stream)}
}

// streamFor returns the stream for a job, creating it on first use. Both the
// producer (Writer) and consumers (Subscribe) may arrive first, so either one
// can create it.
func (b *Broker) streamFor(jobID string) *stream {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.streams[jobID]
	if !ok {
		st = newStream()
		b.streams[jobID] = st
	}
	return st
}

// Writer returns an io.WriteCloser that records and broadcasts a job's output.
// Newline-terminated input becomes one log line each. Close flushes any
// trailing partial line and ends the stream, which signals every subscriber
// that the job is done.
func (b *Broker) Writer(jobID string) io.WriteCloser {
	return &writer{stream: b.streamFor(jobID)}
}

// Subscribe returns the lines produced so far (history) plus a channel of
// future lines. The channel is closed when the job's stream ends. The returned
// cancel func must be called when the subscriber is finished (e.g. the client
// disconnected) to release resources.
func (b *Broker) Subscribe(jobID string) (history []string, lines <-chan string, cancel func()) {
	return b.streamFor(jobID).subscribe()
}

// stream is the per-job state: the full line history and the set of live
// subscriber channels.
type stream struct {
	mu     sync.Mutex
	lines  []string
	subs   map[chan string]struct{}
	closed bool
}

func newStream() *stream {
	return &stream{subs: make(map[chan string]struct{})}
}

// publish appends a line to history and delivers it to every subscriber. The
// send is non-blocking: if a subscriber's buffer is full, the line is dropped
// for that subscriber rather than stalling the job's output. History stays
// complete, so a reconnecting client can replay everything.
func (s *stream) publish(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	for ch := range s.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// closeStream marks the stream done and closes every subscriber channel, which
// ends each subscriber's receive loop. It is idempotent.
func (s *stream) closeStream() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for ch := range s.subs {
		close(ch)
	}
	s.subs = nil
}

func (s *stream) subscribe() ([]string, <-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Copy history under the lock so we capture an exact point-in-time snapshot
	// with no gap between "what we replay" and "what we'll stream".
	history := make([]string, len(s.lines))
	copy(history, s.lines)

	ch := make(chan string, subscriberBuffer)

	// If the job already finished, there are no future lines. Hand back an
	// already-closed channel so the caller's receive loop ends immediately
	// after consuming history.
	if s.closed {
		close(ch)
		return history, ch, func() {}
	}

	s.subs[ch] = struct{}{}

	// cancel removes and closes this subscriber's channel. Guarded by the map
	// membership check so it can't double-close (closeStream may have already
	// closed it). All sends/closes happen under s.mu, so a send can never race
	// a close.
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
	return history, ch, cancel
}

// writer turns an io.Writer byte stream into discrete log lines. It is the
// value the scheduler hands to runner.Run as both stdout and stderr; because
// the same writer value is used for both, os/exec guarantees its Write is
// called by at most one goroutine at a time. The mutex makes it safe for any
// other caller too.
type writer struct {
	stream *stream
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (w *writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline yet — keep the partial line buffered for next time.
			w.buf.WriteString(line)
			break
		}
		w.stream.publish(strings.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

func (w *writer) Close() error {
	w.mu.Lock()
	if rest := w.buf.String(); rest != "" {
		w.stream.publish(strings.TrimRight(rest, "\r\n"))
		w.buf.Reset()
	}
	w.mu.Unlock()

	w.stream.closeStream()
	return nil
}
