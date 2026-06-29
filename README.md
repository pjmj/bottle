# bottle

[![CI](https://github.com/pjmj/bottle/actions/workflows/ci.yml/badge.svg)](https://github.com/pjmj/bottle/actions/workflows/ci.yml)

A miniature compute-job orchestration platform — submit a command as a job, have
it run on a backend worker pool, and watch its logs stream live. It's the same
shape as a cloud ML platform (a CLI and web UI driving an API that schedules and
runs workloads), built small enough to understand end to end.

Three surfaces, one backend: a **Go API**, a **Go CLI**, and a **React web UI**.

## Features

- Submit jobs over a REST API, a CLI, or a web dashboard
- A bounded **worker pool** runs jobs concurrently with backpressure
- **Live log streaming** (Server-Sent Events) to both the terminal and the browser
- Pluggable **execution backend** behind a `Runner` interface (local subprocess today; container/K8s-ready)
- Pluggable **storage** behind a `Store` interface (SQLite today; Postgres-ready)
- Graceful shutdown, structured logging, 12-factor config
- Tested with the race detector in CI; one-command Docker deploy

## Architecture

```
            ┌─────────────┐         ┌──────────────┐
  React UI ─┤             │         │              │
            │   Go API    ├────────►│   Storage    │  (SQLite — Store interface)
   Go CLI ──┤  (net/http) │         │ (jobs table) │
            └──────┬──────┘         └──────────────┘
                   │ Submit(id)              ▲
                   ▼                         │ state transitions
            ┌─────────────┐         ┌────────┴─────┐
            │  Scheduler  ├────────►│    Runner    │  (local subprocess —
            │ worker pool │  runs   │  interface)  │   Runner interface)
            └──────┬──────┘         └──────────────┘
                   │ output
                   ▼
            ┌─────────────┐  SSE    ┌──────────────┐
            │ Log broker  ├────────►│  subscribers │  (CLI `logs`, browser EventSource)
            │  (pub/sub)  │         │              │
            └─────────────┘         └──────────────┘
```

A job flows: **submitted → `queued` → a worker picks it up → `running` → the
runner executes it, streaming output through the log broker → `succeeded` or
`failed`.**

### Packages

| Package | Responsibility |
|---|---|
| `internal/job` | Domain type and lifecycle states (no dependencies) |
| `internal/store` | `Store` interface + `internal/store/sqlite` implementation |
| `internal/runner` | `Runner` interface + local-subprocess implementation |
| `internal/scheduler` | Worker pool: queue, concurrency, state transitions |
| `internal/logs` | In-memory pub/sub broker for job output |
| `internal/api` | HTTP server, handlers, SSE streaming, CORS, static serving |
| `internal/client` | Go SDK for the API (used by the CLI) |
| `internal/cli` | `cobra` command-line client |
| `cmd/server`, `cmd/bottle` | Binary entrypoints |

## Running it

### Option A — Docker (whole stack, one command)

```sh
docker compose up --build
# open http://localhost:8080
```

### Option B — locally (two terminals)

```sh
# terminal 1: API on :8080
go run ./cmd/server

# terminal 2: frontend dev server on :5173
cd web && npm install && npm run dev
```

### Using the CLI

```sh
go build -o bottle ./cmd/bottle

./bottle submit --follow "echo hello && sleep 2 && echo done"
./bottle list
./bottle get <id>
./bottle logs <id>
```

## Configuration

The server reads these environment variables (all optional):

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `DB_PATH` | `bottle.db` | SQLite database file |
| `STATIC_DIR` | _(unset)_ | If set, also serve the built web UI from this directory |

## Development

```sh
make test    # run all Go tests
make race    # run tests with the race detector
make lint    # golangci-lint
make build   # build both binaries into ./bin
```

CI (GitHub Actions) runs `go vet`, `go test -race`, `golangci-lint`, and the
frontend build on every push and pull request.

## Design notes

- **Interfaces at the seams.** `Store` and `Runner` are interfaces, so SQLite →
  Postgres or subprocess → container is an additive change, not a rewrite. The
  API likewise depends on small consumer-defined interfaces (`Submitter`,
  `LogSubscriber`), which is what lets the handlers be tested against fakes with
  no database or running jobs.
- **Concurrency.** The scheduler is a bounded worker pool over a buffered
  channel (backpressure, not unbounded goroutines). Job execution is tied to a
  cancellable context for graceful shutdown, while state writes use a detached
  context so a job's final status is always recorded — even mid-shutdown.
- **Streaming.** Logs use SSE because it's one-directional and the browser
  supports it natively (`EventSource`). The streaming endpoint clears its write
  deadline per-connection so the global `WriteTimeout` doesn't sever long tails.
- **Pure-Go SQLite** (`modernc.org/sqlite`) means no cgo: the Docker image is a
  fully static binary on Alpine, and cross-compilation just works.
