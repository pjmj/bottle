// Package cli builds the `bottle` command-line client on top of cobra. Command
// definitions live here; the binary in cmd/bottle is a thin main that just
// calls Execute. cobra is the de-facto standard for Go CLIs (kubectl, gh, hugo)
// and gives us subcommands, flags, help text, and shell completion for free —
// worth the dependency for a real CLI, versus hand-rolling with stdlib flag.
package cli

import (
	"context"

	"github.com/spf13/cobra"
)

const defaultServer = "http://localhost:8080"

// Execute builds the root command and runs it with the given context, so a
// SIGINT from main cancels in-flight requests and log streams.
func Execute(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	// server is bound to a persistent flag, so it is set before any
	// subcommand's RunE runs. Subcommands read it via the pointer.
	var server string

	root := &cobra.Command{
		Use:   "bottle",
		Short: "bottle is a client for the bottle job platform",
		Long:  "Submit commands as jobs, list them, inspect them, and stream their logs.",
		// We print errors ourselves in main, and we don't want cobra dumping
		// usage text every time a request fails at runtime.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&server, "server", defaultServer, "base URL of the bottle API")

	root.AddCommand(
		newSubmitCmd(&server),
		newListCmd(&server),
		newGetCmd(&server),
		newLogsCmd(&server),
	)
	return root
}
