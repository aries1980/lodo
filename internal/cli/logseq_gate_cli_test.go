package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deadSetup writes a config pointing at a closed port (connection refused
// is immediate, unlike a timeout) and a credentials file with a token, so
// loadSources succeeds and the Logseq gate is what fails.
func deadSetup(t *testing.T) (confPath, credPath, deadURL string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL = srv.URL + "/api"
	srv.Close()

	confPath = filepath.Join(t.TempDir(), "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
logseq:
  api_url: %s
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 7
  timezone: Europe/London
calendar:
  provider: google
  id: primary
`, deadURL)
	if err := os.WriteFile(confPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	credPath = filepath.Join(t.TempDir(), "state", "lodo", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte(`{"logseq":{"token":"st-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return confPath, credPath, deadURL
}

func TestCheckFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, cred, deadURL := deadSetup(t)

	code, out, errOut := runCLI(t, conf, cred, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down (stdout %q stderr %q)", code, out, errOut)
	}
	if !strings.Contains(errOut, "logseq unavailable") || !strings.Contains(errOut, deadURL) {
		t.Fatalf("stderr must name the dead endpoint: %q", errOut)
	}
	if !strings.Contains(errOut, "HTTP API enabled") {
		t.Fatalf("stderr must hint the fix: %q", errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a refused check prints nothing to stdout: %q", out)
	}
}

func TestForecastFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, cred, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, cred, "forecast")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate, not the calendar: %q", errOut)
	}
}

func TestScheduleFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, cred, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, cred, "schedule")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate: %q", errOut)
	}
}

func TestExplainFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, cred, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, cred, "explain", "foo")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate: %q", errOut)
	}
}

func TestRefreshDoesNotRequireLogseq(t *testing.T) {
	conf, cred, _ := deadSetup(t)

	// Refresh never reads the graph: a dead Logseq endpoint must not
	// surface here. With no google section the failure is the calendar
	// hint, proving the gate was skipped.
	code, _, errOut := runCLI(t, conf, cred, "refresh")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 without google credentials", code)
	}
	if strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("refresh must not probe Logseq: %q", errOut)
	}
	if !strings.Contains(errOut, "google credentials") {
		t.Fatalf("stderr must be the calendar hint: %q", errOut)
	}
}

func TestCheckHintsTokenOnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Access Denied", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	conf, cred := testSetup(t, srv)
	// The config already points at the denying server, so Ping gets the 401.

	code, _, errOut := runCLI(t, conf, cred, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 on denied access", code)
	}
	if !strings.Contains(strings.ToLower(errOut), "denied access") {
		t.Fatalf("stderr must name the denial: %q", errOut)
	}
	if !strings.Contains(errOut, "logseq.token") {
		t.Fatalf("stderr must hint the token: %q", errOut)
	}
}
