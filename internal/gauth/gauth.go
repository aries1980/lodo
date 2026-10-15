// Package gauth is the Google OAuth side of the loopback flow (calendar.md
// Auth): `lodo auth google` runs it once through the config package's file
// seam, and the Calendar client refreshes through its token source. It
// speaks OAuth, not the Calendar API; no calendar calls live here and no
// goroutines, either — one listener read synchronously, one exchange, and,
// in the headless mode, one pasted line.
package gauth

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/config"
)

// Scope is exactly what one Calendar needs in v0.1: read and write events
// on the calendar the config names.
const Scope = "https://www.googleapis.com/auth/calendar.events"

// Endpoint is Google's OAuth endpoints. The client secret goes in the POST
// body (that is what Google serves; AuthStyleInParams for determinism).
var Endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

const (
	// DefaultTimeout caps how long the flow waits for the user to finish
	// the browser round-trip. A flow that outlives it is not hanging on a
	// keystroke; re-running `lodo auth google` is one command.
	DefaultTimeout = 5 * time.Minute
	// idleWindow is how long one accepted connection is given to produce a
	// request. Browsers preconnect at page load, so the first sockets can
	// carry no bytes; they are skipped and the redirect is read from the
	// next connection.
	idleWindow = 1 * time.Second
	// exchangeTimeout bounds the code-exchange call, like the Logseq
	// client's per-call ceiling.
	exchangeTimeout = 30 * time.Second
)

// Result is a completed flow: the tokens Google granted. Only the refresh
// token outlives one run and so only it is persisted.
type Result struct {
	RefreshToken string
}

// Inputs are everything Run looks at. Endpoint is Google's in production
// and tests point it at a fake; Raise surfaces the authorization URL before
// the flow waits behind it, and must not be nil.
type Inputs struct {
	// Client holds the pasted client credentials (config's google section).
	Client config.GoogleCredentials
	// Endpoint is the OAuth issuer to authorize and exchange against.
	Endpoint oauth2.Endpoint
	// Raise presents the authorization URL. The flow only reads a redirect
	// after Raise returns, so raising in a terminal and firing the browser
	// from a test both work.
	Raise func(authURL string) error
	// NoBrowser switches the wait off the loopback redirect: the raised
	// URL is completed on some other device, and the redirect address
	// Google then shows — which will not load off the machine running
	// Lodo — is pasted back one line at a time through Lines. The bare
	// authorization code alone works too. Everything else is shared: the
	// same loopback redirect_uri, PKCE verifier and exchange, so one
	// client serves a desktop and a cloud VM alike.
	NoBrowser bool
	// Lines is where the pasted redirect address (or code) arrives, one
	// line at a time. In paste mode it is required (production passes the
	// process's stdin); in the browser mode it is never read.
	Lines io.Reader
}

// Run executes the loopback flow, RFC 8252 style: bind a TLS-free listener
// on 127.0.0.1 at an ephemeral port, Raise the authorization URL (prompt
// consent and offline access, so the grant is a refresh token and it is
// granted again even if it was before), accept exactly one redirect, and
// exchange the code. The listener closes once the redirect served, so a
// second request on that port finds nothing.
//
// With NoBrowser the loopback wait is replaced by a paste wait: the URL is
// finished elsewhere and Lines carries the redirect address or the code
// back. The redirect_uri, PKCE verifier and exchange stay identical, so an
// SSH-tunnelled redirect into the unchanged listener completes the same
// flow — whichever way the code arrives, it is the same grant.
//
// Missing client credentials, a denied grant and an unpasteable wait are
// refusals/errors, not prompts; the browser-mode wait is capped at
// DefaultTimeout and the paste wait is the user's: Ctrl-C ends it.
func Run(ctx context.Context, in Inputs) (Result, error) {
	if in.Raise == nil {
		return Result{}, fmt.Errorf("google auth: no way to raise the authorization URL")
	}
	if in.Client.ClientID == "" || in.Client.ClientSecret == "" {
		return Result{}, fmt.Errorf(
			"google auth: client_id and client_secret are required (paste them into credentials.json; see `lodo auth google --help`)")
	}
	if in.NoBrowser && in.Lines == nil {
		return Result{}, fmt.Errorf("google auth: paste mode needs somewhere to read the pasted redirect from")
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("google auth: bind the loopback listener: %w", err)
	}
	defer func() { _ = l.Close() }() // a listener's close error is never actionable

	// One cap for the whole browser-mode wait: the listener accepts no
	// connection after it, and no connection holds the port for longer
	// than it. The paste wait is the user's own (Ctrl-C), so the cap does
	// not govern it.
	deadline := time.Now().Add(DefaultTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if tl, ok := l.(*net.TCPListener); ok {
		_ = tl.SetDeadline(deadline) // stays available until then
	}

	redirect := fmt.Sprintf("http://127.0.0.1:%d/", l.Addr().(*net.TCPAddr).Port)
	verifier, err := pkceVerifier()
	if err != nil {
		return Result{}, err
	}
	state, err := pkceVerifier()
	if err != nil {
		return Result{}, err
	}

	conf := &oauth2.Config{
		ClientID:     in.Client.ClientID,
		ClientSecret: in.Client.ClientSecret,
		Endpoint:     in.Endpoint,
		Scopes:       []string{Scope},
		RedirectURL:  redirect,
	}
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	if err := in.Raise(authURL); err != nil {
		return Result{}, fmt.Errorf("google auth: %w", err)
	}

	var code string
	if in.NoBrowser {
		code, err = pastedCode(in.Lines, state)
	} else {
		code, err = redirectToCode(l, deadline, state)
	}
	if err != nil {
		return Result{}, err
	}
	// The code exchange is the flow's one network call on its own behalf;
	// it gets the per-call ceiling the Logseq client gives its calls.
	xctx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()
	tok, err := conf.Exchange(xctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return Result{}, fmt.Errorf("google auth: exchange the authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return Result{}, fmt.Errorf(
			"google auth: Google granted no refresh token; re-run `lodo auth google` (it consents again on purpose)")
	}
	return Result{RefreshToken: tok.RefreshToken}, nil
}

// pkceVerifier returns one fresh opaque string, sized for the PKCE spec's
// lower bound and URL-safe so it travels both as challenge seed and state.
func pkceVerifier() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("google auth: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// pkceChallenge is the S256 challenge for one verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// redirectToCode reads the loopback listener synchronously, one connection
// at a time, until the redirect with the expected state arrives. A request
// with no state (health checks, favicons) is turned away so the listener
// keeps waiting; a mismatched state is a failed wait, not a lesson; missing
// code after a matching state skips. Answering is best effort: closing the
// connection is the part the browser actually needs to move on.
func redirectToCode(l net.Listener, deadline time.Time, state string) (string, error) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return "", fmt.Errorf("google auth: no authorization redirect within %s", DefaultTimeout)
		}
		_ = conn.SetDeadline(deadline) // the listener's cap on this connection too
		code, done, err := redirect(conn, state)
		_ = conn.SetDeadline(time.Time{})
		switch {
		case err != nil:
			return "", err
		case done:
			return code, nil
		}
	}
}

// redirect reads ONE connection from the listener. It answers the browser
// and reports whether a decision was made: done with a code; done with an
// error; or not done, meaning the loop should try the next connection.
func redirect(conn net.Conn, state string) (code string, done bool, err error) {
	defer func() { _ = conn.Close() }()

	// An idle preconnect produces no bytes; give it idleWindow and take the
	// next connection. The overall deadline still applies afterwards.
	if err := conn.SetDeadline(time.Now().Add(idleWindow)); err != nil {
		return "", false, nil // undroppable connection is not ours to fix
	}
	req, rerr := http.ReadRequest(bufio.NewReader(conn))
	if rerr != nil || req == nil || req.Method != http.MethodGet {
		return "", false, nil // idle, malformed, or not a GET: next connection
	}

	q := req.URL.Query()
	if !q.Has("state") {
		answer(conn, http.StatusNotFound, "This port exists for the `lodo auth google` redirect; the rest passes through unanswered.")
		return "", false, nil
	}

	if e := q.Get("error"); e != "" {
		answer(conn, http.StatusBadRequest, "Lodo did not receive authorization. Close this page and run `lodo auth google` again.")
		if desc := q.Get("error_description"); desc != "" {
			return "", true, fmt.Errorf("google auth: Google reported %s: %s", e, desc)
		}
		return "", true, fmt.Errorf("google auth: Google reported %s", e)
	}
	if q.Get("state") != state {
		answer(conn, http.StatusBadRequest, "The redirect does not belong to this `lodo auth google` run. Close this page and start the command again.")
		return "", true, fmt.Errorf("google auth: the redirect's state does not match this run — start `lodo auth google` again")
	}

	code = q.Get("code")
	if code == "" {
		answer(conn, http.StatusBadRequest, "Lodo did not receive an authorization code. Close this page and run `lodo auth google` again.")
		return "", false, nil
	}
	answer(conn, http.StatusOK, "Lodo has your Google authorization code. You can close this page and return to the terminal.")
	return code, true, nil
}

// answer writes one small plain-text response; the browser goes on without
// a body, so a write error is not worth reporting.
func answer(conn net.Conn, status int, text string) {
	body := text + "\n"
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, http.StatusText(status), len(body), body)
}

// pastedCode waits for the authorization to come back by hand, when the
// raised URL is finished on some other device: lines from r are read until
// one carries the code — a full redirect address (the address Google sends
// the browser to still shows in the address bar even though it fails to
// load off its origin, which is exactly what the flow wants copied) or the
// bare code. The state on a full address must match this run; empty lines
// and anything else pass on to the next. Reaching the end without a usable
// code is the error, as is a denied pasted grant.
func pastedCode(r io.Reader, state string) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		code, usable, err := pasted(line, state)
		if err != nil {
			return "", err
		}
		if usable {
			return code, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("google auth: read pasted redirect: %w", err)
	}
	return "", fmt.Errorf("google auth: no authorization code was pasted (paste the redirect address; Ctrl-C ends the wait)")
}

// pasted reads one pasted line: either the full redirect address (an
// absolute URL, whose code and state carry the grant) or the bare code
// alone. usable says whether the line produced a decision; err is a failed
// wait — a denied grant or a mismatched state — the same conditions the
// listener reports, with the same messages.
func pasted(line, state string) (code string, usable bool, err error) {
	if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
		return line, line != "", nil // the bare code, no state to check
	}
	u, perr := url.Parse(line)
	if perr != nil {
		return "", false, nil // unparseable; try the next line
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		if desc := q.Get("error_description"); desc != "" {
			return "", true, fmt.Errorf("google auth: Google reported %s: %s", e, desc)
		}
		return "", true, fmt.Errorf("google auth: Google reported %s", e)
	}
	if s := q.Get("state"); s != "" && s != state {
		return "", true, fmt.Errorf("google auth: the pasted address's state does not match this run — start `lodo auth google` again")
	}
	code = q.Get("code")
	return code, code != "", nil
}

// Persist merges res into the credentials file at path: the google section
// keeps the client pair that was pasted in and gains the refresh token,
// while the logseq section passes through. The file's read and write
// belong to the config package (0600, parent 0700); gauth owns none.
func Persist(path string, res Result) error {
	if res.RefreshToken == "" {
		return fmt.Errorf("google auth: no refresh token to persist")
	}
	c, err := config.ReadCredentials(path)
	if err != nil {
		return err
	}
	g := &config.GoogleCredentials{RefreshToken: res.RefreshToken}
	if c.Google != nil {
		g.ClientID, g.ClientSecret = c.Google.ClientID, c.Google.ClientSecret
	}
	c.Google = g
	return config.SaveCredentials(path, c)
}

// TokenSource builds the token source the Calendar client refreshes
// transparently through: one OAuth client re-exchanging its refresh token.
// The first fetch is lazy, so building it never touches the network; a
// failed refresh surfaces on the client's first call — an operational
// error, never a silently empty mask.
func TokenSource(ctx context.Context, endpoint oauth2.Endpoint, g config.GoogleCredentials) (oauth2.TokenSource, error) {
	conf := &oauth2.Config{
		ClientID:     g.ClientID,
		ClientSecret: g.ClientSecret,
		Endpoint:     endpoint,
		Scopes:       []string{Scope},
	}
	return conf.TokenSource(ctx, &oauth2.Token{RefreshToken: g.RefreshToken}), nil
}
