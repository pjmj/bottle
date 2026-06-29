package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pjmj/bottle/internal/api"
	"github.com/pjmj/bottle/internal/logs"
	"github.com/pjmj/bottle/internal/runner"
	"github.com/pjmj/bottle/internal/scheduler"
	"github.com/pjmj/bottle/internal/store/sqlite"
)

const workerCount = 4

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// 12-factor config: read settings from the environment with sensible
	// defaults, so the same binary runs in dev, in Docker, and in prod without
	// recompilation.
	addr := envOr("ADDR", ":8080")
	dbPath := envOr("DB_PATH", "bottle.db")
	staticDir := os.Getenv("STATIC_DIR") // empty = don't serve the frontend

	// Open the persistent store. This is the ONE place that names a concrete
	// database; swapping to Postgres later means changing only this line.
	st, err := sqlite.New(dbPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	// The in-memory log broker: captures job output and fans it out to live
	// subscribers. Shared between the scheduler (producer) and API (consumer).
	logBroker := logs.New()

	// The execution backend (local subprocesses) and the worker pool that
	// drives jobs through it.
	sched := scheduler.New(st, runner.NewLocalRunner(), logBroker, logger, workerCount)

	// ctx is cancelled on SIGINT/SIGTERM and is the root of graceful shutdown:
	// it stops the workers and kills any job currently executing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sched.Start(ctx)

	// Build the API server, optionally also serving the built frontend.
	var opts []api.Option
	if staticDir != "" {
		opts = append(opts, api.WithStaticDir(staticDir))
		logger.Info("serving frontend", "dir", staticDir)
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      api.NewServer(st, sched, logBroker, logger, opts...),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	// Shutdown order matters: first stop accepting new HTTP requests (and let
	// in-flight ones finish)...
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}

	// ...then wait for the worker pool to drain. ctx is already cancelled, so
	// workers exit once their current job returns.
	sched.Wait()
	logger.Info("server stopped cleanly")
}

// envOr returns the value of the environment variable key, or def if it is
// unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
