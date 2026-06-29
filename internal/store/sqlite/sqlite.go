// Package sqlite is a SQLite-backed implementation of store.Store. It uses the
// pure-Go driver modernc.org/sqlite, which needs no C compiler (cgo) — that
// matters on Windows and makes cross-compilation trivial.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	// Imported for its side effect only: this registers the "sqlite" driver
	// with database/sql. The blank identifier says "I want the package's
	// init(), not its exported names."
	_ "modernc.org/sqlite"

	"github.com/pjmj/bottle/internal/job"
	"github.com/pjmj/bottle/internal/store"
)

// Compile-time assertion that *Store satisfies store.Store. If a method ever
// drifts from the interface, the build fails here with a clear message instead
// of somewhere far away at the call site.
var _ store.Store = (*Store)(nil)

// timeFormat is how we serialize timestamps into SQLite's TEXT columns.
// RFC3339 with nanoseconds is sortable as text and human-readable in the DB.
const timeFormat = time.RFC3339Nano

// Store holds the database handle. *sql.DB is itself a connection pool, so a
// single Store is safe for concurrent use by many goroutines.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id          TEXT PRIMARY KEY,
	command     TEXT NOT NULL,
	status      TEXT NOT NULL,
	exit_code   INTEGER,
	created_at  TEXT NOT NULL,
	started_at  TEXT,
	finished_at TEXT
);`

// New opens (or creates) the SQLite database at dsn and ensures the schema
// exists. dsn is a file path (e.g. "bottle.db") or ":memory:" for an ephemeral
// database.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite permits only a single writer at a time. Capping the pool at one
	// connection serializes access and avoids "database is locked" errors. It
	// also keeps a ":memory:" database alive: each new connection would
	// otherwise get its own separate in-memory database.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database connections.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, j *job.Job) error {
	// Parameterized query (the ? placeholders): values are sent separately
	// from the SQL text, so user-supplied data can never be interpreted as
	// SQL. This is the correct defense against SQL injection.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, command, status, exit_code, created_at, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Command, string(j.Status),
		nullInt(j.ExitCode),
		j.CreatedAt.UTC().Format(timeFormat),
		nullTime(j.StartedAt),
		nullTime(j.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*job.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, command, status, exit_code, created_at, started_at, finished_at
		 FROM jobs WHERE id = ?`, id)

	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		// Translate the database-specific "no rows" into our domain error, so
		// callers depend on store.ErrNotFound rather than database/sql.
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return j, nil
}

func (s *Store) List(ctx context.Context) ([]*job.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, command, status, exit_code, created_at, started_at, finished_at
		 FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*job.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	// rows.Err() surfaces an error that ended the iteration early (e.g. a
	// dropped connection). Without this check, a partial result could look
	// like a complete one.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return jobs, nil
}

func (s *Store) Update(ctx context.Context, j *job.Job) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs
		 SET command = ?, status = ?, exit_code = ?, started_at = ?, finished_at = ?
		 WHERE id = ?`,
		j.Command, string(j.Status), nullInt(j.ExitCode),
		nullTime(j.StartedAt), nullTime(j.FinishedAt), j.ID,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// rowScanner is the common Scan method shared by *sql.Row (from QueryRow) and
// *sql.Rows (from Query). Accepting this small interface lets scanJob handle
// both single-row and multi-row reads.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(sc rowScanner) (*job.Job, error) {
	var (
		j          job.Job
		status     string
		exitCode   sql.NullInt64
		createdAt  string
		startedAt  sql.NullString
		finishedAt sql.NullString
	)
	// SQL NULLs cannot scan into plain Go types, so nullable columns scan into
	// sql.Null* wrappers that carry a Valid flag.
	if err := sc.Scan(&j.ID, &j.Command, &status, &exitCode, &createdAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}

	j.Status = job.Status(status)

	if exitCode.Valid {
		v := int(exitCode.Int64)
		j.ExitCode = &v
	}

	created, err := time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	j.CreatedAt = created

	if j.StartedAt, err = parseNullTime(startedAt); err != nil {
		return nil, fmt.Errorf("parse started_at: %w", err)
	}
	if j.FinishedAt, err = parseNullTime(finishedAt); err != nil {
		return nil, fmt.Errorf("parse finished_at: %w", err)
	}
	return &j, nil
}

// nullInt and nullTime convert nil pointers into SQL NULL (Go nil) and non-nil
// pointers into their stored value, so the database faithfully represents
// "not set yet".
func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullTime(p *time.Time) any {
	if p == nil {
		return nil
	}
	return p.UTC().Format(timeFormat)
}

func parseNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeFormat, ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
