package cli

import (
	"bytes"
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

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/config"
)

// The headless flow, all the way through the CLI: the raised URL is printed
// to stdout, the pasted redirect comes back through "stdin", and the
// refresh token lands in the credentials file at 0600.
func TestAuthGoogleNoLaunchBrowser(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	credPath := filepath.Join(dir, "lodo", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath,
		[]byte(`{"logseq":{"token":"st-token"},"google":{"client_id":"cid","client_secret":"secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// The token endpoint of a fake Google; it records the exchange to
	// prove the flow's own mechanics still ran.
	exchanges := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		exchanges++
		if r.Form.Get("grant_type") != "authorization_code" ||
			r.Form.Get("code") != "auth-code" ||
			r.Form.Get("code_verifier") == "" ||
			!strings.HasPrefix(r.Form.Get("redirect_uri"), "http://127.0.0.1:") ||
			r.Form.Get("client_id") != "cid" {
			t.Errorf("unshaped exchange: %v", r.Form)
			http.Error(w, "bad exchange", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "1//access", "refresh_token": "1//granted", "expires_in": 3600, "token_type": "Bearer",
		})
	}))
	t.Cleanup(srv.Close)
	oldEndpoint := googleEndpoint
	googleEndpoint = oauth2.Endpoint{
		AuthURL:   srv.URL + "/authorize",
		TokenURL:  srv.URL + "/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
	t.Cleanup(func() { googleEndpoint = oldEndpoint })

	// The paste input: the URL the raise printed is parsed from inside the
	// stdout capture and fed back synchronously, so the test stays on one
	// goroutine.
	lines := &bytes.Buffer{}
	runFeed := stdin
	stdin = func() io.Reader { return lines }
	t.Cleanup(func() { stdin = runFeed })

	stderr := &strings.Builder{}
	code := Run([]string{"auth", "google", "--no-launch-browser"}, newFeeder(t, lines), stderr)
	if code != 0 {
		t.Fatalf("auth google --no-launch-browser: exit %d, stderr %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr is not empty: %q", stderr.String())
	}
	if exchanges != 1 {
		t.Fatalf("token exchanges = %d, want 1", exchanges)
	}

	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %04o, want 0600", got)
	}
	raw, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Google struct {
			RefreshToken string `json:"refresh_token"`
			ClientID     string `json:"client_id"`
		} `json:"google"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("stored file is not JSON: %v (%s)", err, raw)
	}
	if stored.Google.RefreshToken != "1//granted" || stored.Google.ClientID != "cid" {
		t.Fatalf("google section = %+v", stored.Google)
	}
	if _, err := config.LoadCredentials(credPath); err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
}

// newFeeder wraps an io.Writer: every write whose text carries a raised
// authorization URL is answered with the redirect line the pasting user
// would give back.
func newFeeder(t *testing.T, lines *bytes.Buffer) io.Writer {
	return writeFunc(func(p []byte) (int, error) {
		for _, line := range strings.Split(string(p), "\n") {
			if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
				continue
			}
			u, err := url.Parse(line)
			if err != nil || u.Query().Get("state") == "" {
				continue
			}
			_, port, err := net.SplitHostPort(strings.TrimPrefix(u.Query().Get("redirect_uri"), "http://"))
			if err != nil {
				continue
			}
			fmt.Fprintf(lines, "http://127.0.0.1:%s/?code=auth-code&state=%s\n",
				port, url.QueryEscape(u.Query().Get("state")))
		}
		return len(p), nil
	})
}

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }
