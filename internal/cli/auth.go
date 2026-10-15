// The auth command: the one-time Google OAuth flow whose refresh token the
// Calendar client renews with. The flow's mechanics live in internal/gauth;
// this file composes it with the credentials file and the terminal.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/gauth"
)

// stdin is where paste mode reads the pasted redirects from. It is a var so
// a test can hand the flow its own lines instead of the process's standard
// input.
var stdin = func() io.Reader { return os.Stdin }

// googleEndpoint is the OAuth issuer the auth flow authorize- and
// exchanges against — Google's in production, an httptest fake in tests.
var googleEndpoint = gauth.Endpoint

// runAuthGoogle implements `lodo auth google`: read the credentials file
// (0600), demand the pasted client pair, run the loopback flow, and store
// the refresh token through the config package's write seam. Missing
// pieces exit 1 with the hint; the flow never asks a question of a user
// who cannot answer it.
func runAuthGoogle(opts *options, noBrowser bool) error {
	path, err := config.DefaultCredentialsPath()
	if err != nil {
		return &exitError{code: codeError, err: fmt.Errorf("locate the credentials file: %w", err)}
	}
	creds, err := config.ReadCredentials(path)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	client, err := creds.GoogleSetup()
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	res, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:    client,
		Endpoint:  googleEndpoint,
		Raise:     raise(opts, noBrowser),
		NoBrowser: noBrowser,
		Lines:     stdin(),
	})
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	if err := gauth.Persist(path, res); err != nil {
		return &exitError{code: codeError, err: err}
	}
	_, _ = fmt.Fprintf(opts.out, "Google authorization stored in %s.\n", path)
	return nil
}

// raise surfaces the authorization URL before the flow waits behind it. In
// the browser flow --verbose prints it and xdg-open is tried best effort
// (the opened process is its own; it is reaped in passing); in the headless
// flow the URL must be printed, because the grant is finished on another
// device.
func raise(opts *options, noBrowser bool) func(string) error {
	return func(authURL string) error {
		switch {
		case noBrowser:
			_, _ = fmt.Fprintf(opts.out,
				"Finish Google's authorization on any device: open\n\n%s\n\n"+
					"Afterwards the browser tries an address starting with http://127.0.0.1 —\n"+
					"on this or any other device that will not load, which is expected.\n"+
					"Copy the whole address from there and paste it here on one line; the\n"+
					"bare authorization code also works. (Google's codes expire quickly,\n"+
					"and Ctrl-C ends the wait.)\n",
				authURL)
		case opts.verbose:
			_, _ = fmt.Fprintf(opts.out, "Authorize in a browser at:\n%s\nWaiting for the redirect…\n", authURL)
		default:
			if err := xdgOpen(authURL); err == nil {
				_, _ = fmt.Fprintln(opts.out,
					"Opened the Google authorization page. Finish there and the flow continues here (waiting for the redirect…).")
			} else {
				_, _ = fmt.Fprintf(opts.out,
					"no browser on this machine (%s). Open this URL yourself — on an SSH-tunnelled redirect\n"+
						"the flow completes here as well — or re-run with --no-launch-browser to finish the\n"+
						"grant on any device and paste the redirect back:\n%s\n",
					err, authURL)
			}
		}
		return nil
	}
}

// xdgOpen hands the URL to the desktop's opener and reaps the dispatch
// process; the browser it became is xdg-open's own, not ours to wait on.
func xdgOpen(authURL string) error {
	cmd := exec.Command("xdg-open", authURL)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait() // the dispatcher exits at once; its exit status is not actionable
	return nil
}
