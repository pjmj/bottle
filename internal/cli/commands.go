package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/pjmj/bottle/internal/client"
	"github.com/pjmj/bottle/internal/job"
)

// requestTimeout bounds the quick request/response commands. Streaming logs is
// deliberately excluded — it uses the command's context directly.
const requestTimeout = 10 * time.Second

func newSubmitCmd(server *string) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "submit <command>...",
		Short: "Submit a command to run as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Join the args so `bottle submit echo hello` sends "echo hello".
			command := strings.Join(args, " ")
			c := client.New(*server)

			ctx, cancel := context.WithTimeout(cmd.Context(), requestTimeout)
			defer cancel()
			j, err := c.Submit(ctx, command)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "submitted job %s (%s)\n", j.ID, j.Status)

			if follow {
				// Stream until the job ends or the user hits Ctrl+C (which
				// cancels cmd.Context()). No timeout here — jobs can run long.
				return c.StreamLogs(cmd.Context(), j.ID, cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream the job's logs after submitting")
	return cmd
}

func newListCmd(server *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), requestTimeout)
			defer cancel()
			jobs, err := client.New(*server).List(ctx)
			if err != nil {
				return err
			}

			// tabwriter aligns columns regardless of content width — the
			// standard way to print clean tabular CLI output.
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tEXIT\tCREATED\tCOMMAND")
			for _, j := range jobs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					j.ID, j.Status, exitCodeStr(j),
					j.CreatedAt.Format(time.RFC3339), truncate(j.Command, 40))
			}
			return tw.Flush()
		},
	}
}

func newGetCmd(server *string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show details for a single job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), requestTimeout)
			defer cancel()
			j, err := client.New(*server).Get(ctx, args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "ID:       %s\n", j.ID)
			fmt.Fprintf(out, "Status:   %s\n", j.Status)
			fmt.Fprintf(out, "Command:  %s\n", j.Command)
			fmt.Fprintf(out, "Exit:     %s\n", exitCodeStr(j))
			fmt.Fprintf(out, "Created:  %s\n", j.CreatedAt.Format(time.RFC3339))
			if j.StartedAt != nil {
				fmt.Fprintf(out, "Started:  %s\n", j.StartedAt.Format(time.RFC3339))
			}
			if j.FinishedAt != nil {
				fmt.Fprintf(out, "Finished: %s\n", j.FinishedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func newLogsCmd(server *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logs <id>",
		Short: "Stream a job's logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// No timeout: follow the stream until it ends or Ctrl+C cancels the
			// context inherited from main.
			return client.New(*server).StreamLogs(cmd.Context(), args[0], cmd.OutOrStdout())
		},
	}
}

// exitCodeStr renders the optional exit code, showing "-" when it isn't set yet
// (mirroring the *int "not set vs. zero" distinction from the domain model).
func exitCodeStr(j *job.Job) string {
	if j.ExitCode == nil {
		return "-"
	}
	return strconv.Itoa(*j.ExitCode)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
