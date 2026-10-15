package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

const (
	codeOK    = 0
	codeError = 1
	// codeValidation is exit 2: validation failures under --strict (cli.md).
	codeValidation = 2
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

type options struct {
	json    bool
	verbose bool
	config  string

	// out receives command output; Run wires in the process stdout.
	out io.Writer
}

// Run builds the command tree, executes it and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, "lodo:", err)

		var ee *exitError
		if errors.As(err, &ee) {
			return ee.code
		}

		return codeError
	}

	return codeOK
}

func newRootCmd() *cobra.Command {
	opts := &options{}

	root := &cobra.Command{
		Use:           "lodo",
		Short:         "Schedule Logseq tasks onto one Google Calendar",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "machine-readable output")
	root.PersistentFlags().BoolVar(&opts.verbose, "verbose", false, "explain placement decisions")
	root.PersistentFlags().StringVar(&opts.config, "config", "", "config file (default: $XDG_CONFIG_HOME/lodo/config.yaml)")

	root.AddCommand(
		newCheckCmd(opts),
		newForecastCmd(opts),
		newScheduleCmd(opts),
		newRefreshCmd(opts),
		newAuthCmd(opts),
	)

	return root
}

func newCheckCmd(opts *options) *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate the graph and local state; no side effects",
		RunE: func(c *cobra.Command, _ []string) error {
			opts.out = c.OutOrStdout()
			return runCheck(opts, strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "treat reported problems as a non-zero exit")

	return cmd
}

func newForecastCmd(opts *options) *cobra.Command {
	var overrides configOverrides

	cmd := &cobra.Command{
		Use:   "forecast",
		Short: "Print the Schedule that would be written; a dry run",
		RunE: func(c *cobra.Command, _ []string) error {
			opts.out = c.OutOrStdout()
			return runForecast(opts, overrides, opts.verbose)
		},
	}
	addSchedulingOverrideFlags(cmd, &overrides)

	return cmd
}

func newScheduleCmd(opts *options) *cobra.Command {
	var (
		dryRun    bool
		strict    bool
		overrides configOverrides
	)

	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Reconcile, compute the Schedule and write it to the Calendar",
		RunE: func(_ *cobra.Command, _ []string) error {
			return notImplemented("schedule")
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "same as forecast")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat reported problems as a non-zero exit")
	addSchedulingOverrideFlags(cmd, &overrides)

	return cmd
}

func newRefreshCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Fold calendar edits into local state without recomputing",
		RunE: func(_ *cobra.Command, _ []string) error {
			return notImplemented("refresh")
		},
	}
}

func newAuthCmd(opts *options) *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage provider credentials",
	}
	auth.AddCommand(&cobra.Command{
		Use:   "google",
		Short: "Run the loopback OAuth flow and store the refresh token",
		RunE: func(_ *cobra.Command, _ []string) error {
			return notImplemented("auth google")
		},
	})

	return auth
}

type configOverrides struct {
	windowDays int
	timezone   string
}

func addSchedulingOverrideFlags(cmd *cobra.Command, overrides *configOverrides) {
	cmd.Flags().IntVar(&overrides.windowDays, "window-days", 0, "override scheduling.window_days for one run")
	cmd.Flags().StringVar(&overrides.timezone, "timezone", "", "override scheduling.timezone for one run")
}

func notImplemented(name string) error {
	return &exitError{code: codeError, err: fmt.Errorf("lodo %s: not implemented yet", name)}
}
