package gauth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/gauth"
)

// The pair a user pastes into credentials.json before running the flow.
const (
	testClientID     = "pasted-client-id"
	testClientSecret = "pasted-client-secret"
)

func credsPair() config.GoogleCredentials {
	return config.GoogleCredentials{ClientID: testClientID, ClientSecret: testClientSecret}
}

func credsBody() string {
	return fmt.Sprintf(
		`{"logseq":{"token":"st-token"},"google":{"client_id":%q,"client_secret":%q}}`,
		testClientID, testClientSecret)
}

func writeCreds(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tokenExchanges is the fake token endpoint one flow talks to. It remembers
// every exchange accepted, in arrival order, so tests can assert the wire
// shapes: an authorization-code exchange carries the code, the verifier and
// the loopback redirect, and a later refresh carries the refresh token.
type tokenExchanges struct {
	code, refresh []url.Values
	// failCode answers the authorization-code grant with an error.
	failCode bool
	// omitRefresh grants no refresh token in the code grant's response.
	omitRefresh bool
	// revokedRefresh answers the refresh grant with Google's revocation
	// error, the way an expired or revoked grant arrives.
	revokedRefresh bool
}

func (s *tokenExchanges) endpoint(t *testing.T) oauth2.Endpoint {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST the token endpoint", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, "form", http.StatusBadRequest)
			return
		}
		switch grant := r.Form.Get("grant_type"); grant {
		case "authorization_code":
			if r.Form.Get("code") == "" || r.Form.Get("redirect_uri") == "" ||
				r.Form.Get("client_id") != testClientID || r.Form.Get("client_secret") != testClientSecret {
				t.Errorf("exchange missed the shape: code %q redirect %q client %q",
					r.Form.Get("code"), r.Form.Get("redirect_uri"), r.Form.Get("client_id"))
				http.Error(w, "bad exchange", http.StatusBadRequest)
				return
			}
			if s.failCode {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"the code expired"}`))
				return
			}
			s.code = append(s.code, cloneForm(r.Form))
			writeToken(t, w, s.omitRefresh)
		case "refresh_token":
			if s.revokedRefresh {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
				return
			}
			if r.Form.Get("refresh_token") != "1//stored-refresh-token" {
				t.Errorf("refresh token = %q", r.Form.Get("refresh_token"))
				http.Error(w, "unknown refresh token", http.StatusBadRequest)
				return
			}
			s.refresh = append(s.refresh, cloneForm(r.Form))
			writeToken(t, w, false)
		default:
			t.Errorf("unknown grant_type %q", grant)
			http.Error(w, "unknown grant_type", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return oauth2.Endpoint{
		AuthURL:   srv.URL + "/authorize",
		TokenURL:  srv.URL + "/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
}

func writeToken(t *testing.T, w http.ResponseWriter, omitRefresh bool) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"access_token": "1//access", "token_type": "Bearer", "expires_in": 3600}
	if !omitRefresh {
		body["refresh_token"] = "1//stored-refresh-token"
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("token response: %v", err)
	}
}

func cloneForm(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		out[k] = append([]string{}, vs...)
	}
	return out
}

// raiseHalf is the browser half of one mode, handed the raised shape by the
// Raise hook. The browser-mode half fires the redirect against the flow's
// loopback listener; the paste-mode half writes the line the user would
// type into the flow's paste input.
type raiseHalf func(q url.Values, port string)

// capture records what a run raised: the authorization URL and the
// loopback port behind it, for after-the-run assertions.
type capture struct {
	t       *testing.T
	authURL string
	port    string
}

// raise is a Raise func for Run.
func (c *capture) raise(half raiseHalf) func(string) error {
	return func(authURL string) error {
		q, port := c.parse(authURL)
		c.authURL, c.port = authURL, port
		if half != nil {
			half(q, port)
		}
		return nil
	}
}

// parse validates the shape of one raised authorization URL and returns
// its query plus the loopback port behind its redirect. The URL is all the
// browser learns, so it is where the flow's ask quietly hides: the pasted
// client, response type, calendar-events scope, offline access with a
// re-consent (so the grant survives a revocation and a re-run), and an S256
// code challenge.
func (c *capture) parse(authURL string) (url.Values, string) {
	c.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		c.t.Fatalf("raise authURL %s: %v", authURL, err)
	}
	q := u.Query()
	for _, want := range []string{"client_id", "response_type", "scope", "state", "redirect_uri", "code_challenge", "code_challenge_method"} {
		if !q.Has(want) {
			c.t.Fatalf("authURL is missing %s: %s", want, authURL)
		}
	}
	if q.Get("response_type") != "code" {
		c.t.Fatalf("response_type = %q, want code (%s)", q.Get("response_type"), authURL)
	}
	if q.Get("client_id") != testClientID {
		c.t.Fatalf("client_id = %q, want the pasted pair's id (%s)", q.Get("client_id"), authURL)
	}
	if !strings.Contains(q.Get("scope"), gauth.Scope) {
		c.t.Fatalf("scope = %q, want calendar.events (%s)", q.Get("scope"), authURL)
	}
	if q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		c.t.Fatalf("the grant must survive a re-run: access_type=%q prompt=%q (%s)",
			q.Get("access_type"), q.Get("prompt"), authURL)
	}
	if q.Get("code_challenge_method") != "S256" {
		c.t.Fatalf("code_challenge_method = %q, want S256 (%s)", q.Get("code_challenge_method"), authURL)
	}
	_, port, err := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(q.Get("redirect_uri"), "http://"), "/"))
	if err != nil || port == "" {
		c.t.Fatalf("redirect_uri %q: no loopback port", q.Get("redirect_uri"))
	}
	return q, port
}

// query returns the raised URL's query for after-the-run assertions.
func (c *capture) query() url.Values {
	c.t.Helper()
	u, err := url.Parse(c.authURL)
	if err != nil {
		c.t.Fatalf("re-parse raised authURL %s: %v", c.authURL, err)
	}
	return u.Query()
}

// fireRedirect carries the grant back through the flow's listener, the way
// a browser on this machine — or an SSH tunnel into it — would. The GET is
// fired off, not waited for: the flow answers only once Raise has
// returned, so a synchronous call would have the browser block the accept
// loop it is meant to feed.
func fireRedirect(query string) raiseHalf {
	return func(q url.Values, port string) {
		full := fmt.Sprintf("http://127.0.0.1:%s/?%s", port, withState(query, q))
		go func() {
			resp, err := http.Get(full)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
}

// fireHappy is the checked variant: the redirect's page is asserted the way
// a user reads it.
func fireHappy(t *testing.T, query string) raiseHalf {
	return func(q url.Values, port string) {
		full := fmt.Sprintf("http://127.0.0.1:%s/?%s", port, withState(query, q))
		go func() {
			resp, err := http.Get(full)
			if err != nil {
				t.Errorf("redirect GET: %v", err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "authorization code") {
				t.Errorf("the flow answered the redirect with HTTP %d %q",
					resp.StatusCode, strings.TrimSpace(string(body)))
			}
		}()
	}
}

// withState appends the raised URL's state to a raw redirect query, so
// tests do not hardcode the flow's CSRF token — and a query carrying its
// own (wrong) state keeps winning, the way a stale tab would.
func withState(query string, q url.Values) string {
	return fmt.Sprintf("%s&state=%s", query, url.QueryEscape(q.Get("state")))
}

// pkceHolds asserts the exchange's verifier hashes to the challenge the
// raised URL carried: that link is the whole PKCE claim.
func pkceHolds(t *testing.T, raised, exchange url.Values) {
	t.Helper()
	sum := sha256.Sum256([]byte(exchange.Get("code_verifier")))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != raised.Get("code_challenge") {
		t.Fatalf("verifier does not hash to the raised challenge: %q vs %q", got, raised.Get("code_challenge"))
	}
}

// pasteInputs builds the headless Inputs: the flow is told no browser is
// coming, and the user's one-line pastes go through lines. The half writes
// what another device would have handed back — the redirect address with
// the raised state — or a bare code, so the state here is the flow's own.
func pasteInputs(t *testing.T, s *tokenExchanges, c *capture, lines *bytes.Buffer, half func(state, port string) string) gauth.Inputs {
	return gauth.Inputs{
		Client:    credsPair(),
		Endpoint:  s.endpoint(t),
		Raise:     c.raise(func(q url.Values, port string) { lines.WriteString(half(q.Get("state"), port)) }),
		NoBrowser: true,
		Lines:     lines,
	}
}

// pastedAddress is the redirect address the browser shows elsewhere after a
// grant: Google echoes back the URL it was told to redirect to, which on
// the other device does not load but does exactly carry the grant now.
func pastedAddress(state, port string) string {
	return fmt.Sprintf("http://127.0.0.1:%s/?code=auth-code&state=%s\n", port, state)
}

func TestRunLoopbackFlow(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	res, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(fireHappy(t, "code=auth-code")),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RefreshToken != "1//stored-refresh-token" {
		t.Fatalf("RefreshToken = %q", res.RefreshToken)
	}

	// Exactly one exchange, with the loopback redirect and a verifier that
	// hashes to the raised challenge.
	if n := len(s.code); n != 1 {
		t.Fatalf("code exchanges = %d, want 1", n)
	}
	form := s.code[0]
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "auth-code" {
		t.Fatalf("exchange = %v", form)
	}
	if !strings.HasPrefix(form.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("exchange redirect_uri = %q, want the loopback listener", form.Get("redirect_uri"))
	}
	pkceHolds(t, c.query(), form)

	// Accept exactly one redirect: the listener is gone now.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/?code=second&state=late", c.port))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the listener accepted a second redirect")
	}
}

func TestRunLoopbackDeniesGrant(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(fireRedirect("error=access_denied&error_description=no%20thanks")),
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("Run err = %v, want the denial", err)
	}
	// The denial is a stop, and the listener is gone.
	if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/", c.port)); err == nil {
		t.Fatal("the listener survived a denied grant")
	}
}

func TestRunLoopbackStateMismatchStops(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(fireRedirect("code=auth-code&state=from-a-different-run")),
	})
	if err == nil || !strings.Contains(err.Error(), "state does not match") {
		t.Fatalf("Run err = %v, want the state mismatch", err)
	}
	if len(s.code) != 0 {
		t.Fatalf("exchanges = %d, want none", len(s.code))
	}
}

func TestRunLoopbackExchangeFails(t *testing.T) {
	s := &tokenExchanges{failCode: true}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(fireRedirect("code=auth-code")),
	})
	if err == nil || !strings.Contains(err.Error(), "exchange the authorization code") {
		t.Fatalf("Run err = %v, want the exchange failure", err)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("the failure should carry Google's error: %v", err)
	}
	if len(s.code) != 0 {
		t.Fatalf("exchanges recorded = %d, want none", len(s.code))
	}
}

func TestRunLoopbackNoRefreshToken(t *testing.T) {
	s := &tokenExchanges{omitRefresh: true}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(fireRedirect("code=auth-code")),
	})
	if err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("Run err = %v, want the missing refresh token", err)
	}
}

func TestRunLoopbackTimesOut(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := gauth.Run(ctx, gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(nil), // nothing ever comes back
	})
	if err == nil || !strings.Contains(err.Error(), "no authorization redirect within") {
		t.Fatalf("Run err = %v, want the wait to expire", err)
	}
}

func TestRunLoopbackSkipsIdleConnections(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	// Browsers preconnect: one silent socket lands before the redirect
	// does, and the flow must skip it rather than wait on it forever.
	half := func(q url.Values, port string) {
		idle, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", port))
		if err != nil {
			t.Fatalf("idle dial: %v", err)
		}
		defer func() { _ = idle.Close() }()
		time.Sleep(100 * time.Millisecond)
		fireRedirect("code=auth-code")(q, port)
	}
	res, err := gauth.Run(context.Background(), gauth.Inputs{
		Client:   credsPair(),
		Endpoint: s.endpoint(t),
		Raise:    c.raise(half),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RefreshToken != "1//stored-refresh-token" {
		t.Fatalf("RefreshToken = %q", res.RefreshToken)
	}
	if n := len(s.code); n != 1 {
		t.Fatalf("code exchanges = %d, want 1", n)
	}
}

func TestRunWantsItsInputs(t *testing.T) {
	s := &tokenExchanges{}
	endpoint := s.endpoint(t)

	// No way to raise the URL.
	_, err := gauth.Run(context.Background(), gauth.Inputs{Client: credsPair(), Endpoint: endpoint})
	if err == nil || !strings.Contains(err.Error(), "no way to raise") {
		t.Fatalf("Run err = %v, want the missing Raise", err)
	}

	// No pasted client pair.
	c := &capture{t: t}
	_, err = gauth.Run(context.Background(), gauth.Inputs{Endpoint: endpoint, Raise: c.raise(nil)})
	if err == nil || !strings.Contains(err.Error(), "client_id and client_secret are required") {
		t.Fatalf("Run err = %v, want the client pair's refusal", err)
	}

	// Paste mode with nowhere to paste.
	_, err = gauth.Run(context.Background(), gauth.Inputs{
		Client: credsPair(), Endpoint: endpoint, Raise: c.raise(nil), NoBrowser: true,
	})
	if err == nil || !strings.Contains(err.Error(), "paste mode needs") {
		t.Fatalf("Run err = %v, want the missing paste input", err)
	}
}

func TestRunNoBrowserPastedAddress(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	res, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{}, pastedAddress))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RefreshToken != "1//stored-refresh-token" {
		t.Fatalf("RefreshToken = %q", res.RefreshToken)
	}
	if n := len(s.code); n != 1 {
		t.Fatalf("code exchanges = %d, want 1", n)
	}
	form := s.code[0]
	if form.Get("code") != "auth-code" {
		t.Fatalf("exchange code = %q", form.Get("code"))
	}
	if !strings.HasPrefix(form.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("exchange redirect_uri = %q, want the same loopback redirect", form.Get("redirect_uri"))
	}
	pkceHolds(t, c.query(), form)
}

func TestRunNoBrowserBareCode(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	res, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{},
		func(_, _ string) string { return "auth-code\n" }))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RefreshToken != "1//stored-refresh-token" {
		t.Fatalf("RefreshToken = %q", res.RefreshToken)
	}
	if n := len(s.code); n != 1 {
		t.Fatalf("code exchanges = %d, want 1", n)
	}
}

func TestRunNoBrowserToleratesNoise(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	// Blank lines, prose before the paste, and an unparseable address are
	// all skipped on the way to the code.
	res, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{},
		func(_, _ string) string {
			return "\nstill working on it…\nhttp://not the address\n  auth-code  \n"
		}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RefreshToken != "1//stored-refresh-token" {
		t.Fatalf("RefreshToken = %q", res.RefreshToken)
	}
}

func TestRunNoBrowserDeniedPaste(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{},
		func(state, port string) string {
			return fmt.Sprintf("http://127.0.0.1:%s/?error=access_denied&state=%s\n", port, state)
		}))
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("Run err = %v, want the denial", err)
	}
}

func TestRunNoBrowserMismatchedPaste(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{},
		func(_, port string) string {
			return fmt.Sprintf("http://127.0.0.1:%s/?code=auth-code&state=stale-state\n", port)
		}))
	if err == nil || !strings.Contains(err.Error(), "state does not match") {
		t.Fatalf("Run err = %v, want the mismatch", err)
	}
	if len(s.code) != 0 {
		t.Fatalf("exchanges = %d, want none", len(s.code))
	}
}

func TestRunNoBrowserNothingPasted(t *testing.T) {
	s := &tokenExchanges{}
	c := &capture{t: t}
	_, err := gauth.Run(context.Background(), pasteInputs(t, s, c, &bytes.Buffer{},
		func(_, _ string) string { return "" }))
	if err == nil || !strings.Contains(err.Error(), "no authorization code was pasted") {
		t.Fatalf("Run err = %v, want the empty paste", err)
	}
}

func TestPersistMergesAndKeeps0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCreds(t, path, credsBody())
	if err := gauth.Persist(path, gauth.Result{RefreshToken: "1//stored-refresh-token"}); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode after persist = %04o, want 0600", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Logseq struct {
			Token string `json:"token"`
		} `json:"logseq"`
		Google struct {
			RefreshToken string `json:"refresh_token"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"google"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the merged file is not JSON: %v (%s)", err, raw)
	}
	if got.Logseq.Token != "st-token" {
		t.Fatalf("logseq token = %q, want it kept", got.Logseq.Token)
	}
	if got.Google.RefreshToken != "1//stored-refresh-token" ||
		got.Google.ClientID != testClientID || got.Google.ClientSecret != testClientSecret {
		t.Fatalf("google section = %+v", got.Google)
	}

	// The merged file is loop-ready: check and forecast load it as they
	// already did.
	if _, err := config.LoadCredentials(path); err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}

	// An empty result is refused rather than stored as a hole.
	if err := gauth.Persist(path, gauth.Result{}); err == nil {
		t.Fatal("Persist accepted an empty refresh token")
	}
}

func TestPersistCreatesFileFromEnvClient(t *testing.T) {
	t.Setenv("LODO_LOGSEQ_TOKEN", "env-logseq-token")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", testClientID)
	t.Setenv("LODO_GOOGLE_CLIENT_SECRET", testClientSecret)
	t.Setenv("LODO_GOOGLE_REFRESH_TOKEN", "")

	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := gauth.Persist(path, gauth.Result{RefreshToken: "1//env-refresh"}); err != nil {
		t.Fatalf("Persist from env: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Logseq struct {
			Token string `json:"token"`
		} `json:"logseq"`
		Google struct {
			RefreshToken string `json:"refresh_token"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"google"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("persisted file is not JSON: %v (%s)", err, raw)
	}
	if got.Google.RefreshToken != "1//env-refresh" ||
		got.Google.ClientID != testClientID || got.Google.ClientSecret != testClientSecret {
		t.Fatalf("google section = %+v, want env pair plus stored refresh", got.Google)
	}
	if got.Logseq.Token != "" {
		t.Fatalf("logseq token = %q, want empty: an env-only token must not be copied into the file", got.Logseq.Token)
	}
}

func TestTokenSourceRefreshesTransparently(t *testing.T) {
	s := &tokenExchanges{}
	endpoint := s.endpoint(t)
	seen := 0
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer 1//access" {
			t.Errorf("Authorization = %q", got)
			http.Error(w, "who are you", http.StatusUnauthorized)
			return
		}
		seen++
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(resource.Close)

	src, err := gauth.TokenSource(context.Background(), endpoint, config.GoogleCredentials{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		RefreshToken: "1//stored-refresh-token",
	})
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	hc := oauth2.NewClient(context.Background(), src)
	for i := 0; i < 2; i++ {
		resp, err := hc.Get(resource.URL)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	if seen != 2 {
		t.Fatalf("resource hits = %d, want 2", seen)
	}

	// One refresh on the wire: the access token was reused within its
	// expiry, and the client pair travelled in the body.
	if n := len(s.refresh); n != 1 {
		t.Fatalf("refreshes = %d, want 1", n)
	}
	form := s.refresh[0]
	if form.Get("grant_type") != "refresh_token" ||
		form.Get("refresh_token") != "1//stored-refresh-token" ||
		form.Get("client_id") != testClientID {
		t.Fatalf("refresh = %v", form)
	}
	// No scope rides the refresh call: the grant carries its scopes
	// server-side, which x/oauth2 does not restate.
}

func TestTokenSourceRevokedIsAnError(t *testing.T) {
	s := &tokenExchanges{revokedRefresh: true}
	endpoint := s.endpoint(t)

	src, err := gauth.TokenSource(context.Background(), endpoint, config.GoogleCredentials{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		RefreshToken: "1//stored-refresh-token",
	})
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	hc := oauth2.NewClient(context.Background(), src)
	if _, err := hc.Get(endpoint.TokenURL); err == nil {
		t.Fatal("a revoked grant must surface as an error, not a silently usable client")
	}
}
