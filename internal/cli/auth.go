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
	"strings"

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

// runAuthGoogle implements `lodo auth google`: read the credentials (file,
// environment, then CLI flags), demand the client pair, and either store a
// flag-supplied refresh token directly or run the loopback flow to mint one.
// Missing pieces exit 1 with the hint; the flow never asks a question of a
// user who cannot answer it.
func runAuthGoogle(opts *options, ov configOverrides, noBrowser bool) error {
	start := logRunStart(opts, "auth")
	path, err := config.DefaultCredentialsPath()
	if err != nil {
		return &exitError{code: codeError, err: fmt.Errorf("locate the credentials file: %w", err)}
	}
	creds, err := config.ReadCredentials(path)
	if err != nil {
		// A missing file is fine when the flags carry the pair: start from
		// the environment alone so --google-client-id/secret can provision
		// a fresh machine without a file yet.
		if msg := err.Error(); !containsNotFoundMsg(msg) {
			return &exitError{code: codeError, err: err}
		}
		creds = config.CredentialsFromEnv()
	}
	creds = creds.WithCredentialOverrides(ov.credentialOverrides())
	client, err := creds.GoogleSetup()
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	// A flag-supplied refresh token stores directly: no browser round-trip.
	if strings.TrimSpace(ov.googleRefreshToken) != "" {
		if err := gauth.PersistWithClient(path, client, gauth.Result{RefreshToken: strings.TrimSpace(ov.googleRefreshToken)}); err != nil {
			return &exitError{code: codeError, err: err}
		}
		if err := printStored(opts, path); err != nil {
			return err
		}
		logRunFinish(opts, "auth", start, map[string]any{"stored": path})
		return nil
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
	if err := gauth.PersistWithClient(path, client, res); err != nil {
		return &exitError{code: codeError, err: err}
	}
	if err := printStored(opts, path); err != nil {
		return err
	}
	logRunFinish(opts, "auth", start, map[string]any{"stored": path})
	return nil
}

func printStored(opts *options, path string) error {
	st := newStyler(opts.out, opts.disableColors)
	if opts.json {
		if err := printJSON(opts.out, struct {
			Stored string `json:"stored"`
		}{Stored: path}); err != nil {
			return &exitError{code: codeError, err: err}
		}
		return nil
	}
	_, _ = fmt.Fprintln(opts.out, st.success(fmt.Sprintf("Google authorization stored in %s.", path)))
	return nil
}

// containsNotFoundMsg reports the missing-file refusal so a flag-provisioned
// pair can start from the environment when no file exists yet. Any other
// read error (permissions, malformed JSON) still refuses.
func containsNotFoundMsg(msg string) bool {
	return strings.Contains(msg, ": not found (default ") || strings.Contains(msg, ": not found")
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
