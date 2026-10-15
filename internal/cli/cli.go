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
		RunE: func(c *cobra.Command, _ []string) error {
			opts.out = c.OutOrStdout()
			return runSchedule(opts, overrides, dryRun, strict, opts.verbose)
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
		RunE: func(c *cobra.Command, _ []string) error {
			opts.out = c.OutOrStdout()
			return runRefresh(opts)
		},
	}
}

func newAuthCmd(opts *options) *cobra.Command {
	var noBrowser bool

	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage provider credentials",
	}
	cmd := &cobra.Command{
		Use:   "google",
		Short: "Run the loopback OAuth flow and store the refresh token",
		Long: `Store Google's refresh token for ` + "`lodo forecast` and `lodo schedule`" + `, one time.

Before running, create an OAuth client in the Google Cloud console (a
"Desktop app" is the dependable type; its redirect URI is http://127.0.0.1/)
and paste client_id and client_secret into credentials.json beside the
Logseq token, if that is there yet:

  {"google": {"client_id": "…", "client_secret": "…"}}

The flow binds a loopback listener, raises the authorization URL
(calendar.events scope, offline access and a re-consent, so the grant is a
refresh token that survives a revocation) and accepts exactly one
redirect. On a machine that opens a browser, xdg-open takes the URL and
the flow waits; print the URL instead with --verbose.

Google's login is one system for personal Gmail and Google Workspace
accounts: approve with the account whose calendar Lodo should use, and
calendar.id names the one calendar inside it. A Workspace admin's
third-party app policy must allow your own client (the unverified-app
page is normal for a personal client_id), and the Calendar API must be
enabled in the project that owns it.

On a machine with no browser to open — a cloud VM — pass
--no-launch-browser: the URL is printed to finish on any device, and the
redirect address Google then shows (which will not load off the machine)
is pasted back here on one line to exchange; the bare authorization code
works too. The same listener still completes an SSH-tunnelled redirect.
Run the command again after a revocation. Missing client credentials exit
1 with a hint; there are no interactive prompts about them.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			opts.out = c.OutOrStdout()
			return runAuthGoogle(opts, noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-launch-browser", false,
		"headless machines: finish the grant on another device and paste the redirect back here")
	auth.AddCommand(cmd)

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
