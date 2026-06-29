// Package runner defines how a job's command is actually executed, and ships a
// local-subprocess implementation. The execution backend is deliberately
// hidden behind the Runner interface so it can be swapped (a Docker or
// Kubernetes runner later) without touching the scheduler that calls it.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// Runner executes a command and streams its combined output to logs. It
// returns the process exit code.
//
// Note the contract distinction, which the scheduler relies on:
//   - A process that runs and exits non-zero returns (code, nil). A non-zero
//     exit is a *job outcome*, not a runner failure.
//   - A non-nil error means the process could not be run at all (shell missing,
//     context cancelled, etc.).
//
// Runner intentionally takes a plain command string and an io.Writer, not a
// job.Job — it knows nothing about the domain, which keeps it reusable and
// trivial to test.
type Runner interface {
	Run(ctx context.Context, command string, logs io.Writer) (exitCode int, err error)
}

// LocalRunner executes commands as subprocesses on the host, through the
// system shell so that shell features (pipes, &&, quoting) work as a user would
// expect.
type LocalRunner struct {
	Shell string // e.g. "sh" or "cmd"
	Arg   string // the shell's "run this string" flag, e.g. "-c" or "/C"
}

// Compile-time check that *LocalRunner satisfies Runner.
var _ Runner = (*LocalRunner)(nil)

// NewLocalRunner returns a LocalRunner configured for the host operating
// system's default shell.
func NewLocalRunner() *LocalRunner {
	shell, arg := defaultShell()
	return &LocalRunner{Shell: shell, Arg: arg}
}

func defaultShell() (shell, arg string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}

func (r *LocalRunner) Run(ctx context.Context, command string, logs io.Writer) (int, error) {
	// exec.CommandContext ties the process lifetime to ctx: if ctx is
	// cancelled (e.g. server shutdown), the process is killed automatically.
	cmd := exec.CommandContext(ctx, r.Shell, r.Arg, command)

	// Send both stdout and stderr to the same writer so the captured logs read
	// in the order the program produced them, exactly as a terminal would show.
	cmd.Stdout = logs
	cmd.Stderr = logs

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}

	// If ctx was cancelled, that is the true cause regardless of how the
	// process died, so report it as the error.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return -1, ctxErr
	}

	// The process ran but exited non-zero: extract the code and report no
	// error, because the runner did its job.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}

	// Anything else means we never got a running process.
	return -1, fmt.Errorf("run command: %w", err)
}
