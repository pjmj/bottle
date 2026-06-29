// Package api exposes the platform over HTTP. It depends only on the
// store.Store interface, so it neither knows nor cares which database backs it.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/pjmj/bottle/internal/store"
)

// Submitter is the slice of the scheduler the API actually needs: the ability
// to enqueue a job ID for execution. Declaring this interface here, in the
// consumer, rather than importing the concrete scheduler, is the idiomatic Go
// guideline "accept interfaces". It keeps the API decoupled from the scheduler
// and lets tests pass a no-op submitter.
type Submitter interface {
	Submit(ctx context.Context, id string) error
}

// LogSubscriber is the slice of the log broker the API needs: subscribe to a
// job's output and get its history plus a channel of future lines. Faking this
// in tests lets us exercise the streaming handler with no real job running.
type LogSubscriber interface {
	Subscribe(jobID string) (history []string, lines <-chan string, cancel func())
}

// Server holds the API's dependencies and routing. Bundling dependencies on a
// struct (rather than reaching for package-level globals) keeps handlers
// testable: a test constructs a Server with a fake store and a discard logger.
type Server struct {
	store     store.Store
	scheduler Submitter
	logs      LogSubscriber
	logger    *slog.Logger
	mux       *http.ServeMux
	handler   http.Handler // mux wrapped with middleware (CORS)

	staticDir string // if set, serve the built frontend from here
}

// Option configures a Server. Functional options let us add optional behavior
// (like serving static files) without breaking the NewServer signature every
// time — existing callers stay unchanged, new ones opt in.
type Option func(*Server)

// WithStaticDir makes the server also serve the built frontend from dir,
// falling back to index.html for client-side routes. This lets one binary serve
// both the API and the web app — a single deployable artifact.
func WithStaticDir(dir string) Option {
	return func(s *Server) { s.staticDir = dir }
}

// NewServer wires up a Server and registers its routes. It accepts interfaces
// (store.Store, Submitter, LogSubscriber), so callers pass real implementations
// in production or fakes in tests — dependency injection, the plain Go way.
func NewServer(st store.Store, sch Submitter, lb LogSubscriber, logger *slog.Logger, opts ...Option) *Server {
	s := &Server{
		store:     st,
		scheduler: sch,
		logs:      lb,
		logger:    logger,
		mux:       http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.routes()
	// Wrap the router once with middleware. Every request flows through CORS
	// before reaching a route handler.
	s.handler = withCORS(s.mux)
	return s
}

// ServeHTTP lets *Server satisfy http.Handler, so it can be handed straight to
// http.Server. It delegates to the middleware-wrapped handler; routing stays in
// one private place (routes) rather than exposing the mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// routes is the single source of truth for the API surface. Keeping every
// route in one method makes the whole API readable at a glance.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /jobs", s.handleCreateJob)
	s.mux.HandleFunc("GET /jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("GET /jobs/{id}/logs", s.handleJobLogs)

	// The catch-all "/" serves the frontend, if configured. The specific API
	// patterns above always win over "/" in Go 1.22+ routing, so this never
	// shadows them.
	if s.staticDir != "" {
		s.mux.Handle("/", spaHandler(s.staticDir))
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
