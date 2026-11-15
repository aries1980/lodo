package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deadSetup writes a config pointing at a closed port (connection refused
// is immediate, unlike a timeout) and a credentials file with a token, so
// prepare succeeds and the Logseq gate is what fails.
func deadSetup(t *testing.T) (conf string, deadURL string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL = srv.URL + "/api"
	srv.Close()

	dir := t.TempDir()
	conf, err := writeConfig(t, dir, srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeCredentials(t, dir, e2eLogseqCreds); err != nil {
		t.Fatal(err)
	}
	return conf, deadURL
}

func TestCheckFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, deadURL := deadSetup(t)

	code, out, errOut := runCLI(t, conf, "check")
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
	conf, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, "forecast")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate, not the calendar: %q", errOut)
	}
}

func TestScheduleFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, "schedule")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate: %q", errOut)
	}
}

func TestExplainFailsEarlyWhenLogseqDown(t *testing.T) {
	conf, _ := deadSetup(t)

	code, _, errOut := runCLI(t, conf, "explain", "foo")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when Logseq is down", code)
	}
	if !strings.Contains(errOut, "logseq unavailable") {
		t.Fatalf("stderr must be the Logseq gate: %q", errOut)
	}
}

func TestRefreshDoesNotRequireLogseq(t *testing.T) {
	conf, _ := deadSetup(t)

	// Refresh never reads the graph: a dead Logseq endpoint must not
	// surface here. With no google section the failure is the calendar
	// hint, proving the gate was skipped.
	code, _, errOut := runCLI(t, conf, "refresh")
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

	dir := t.TempDir()
	conf, err := writeConfig(t, dir, srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeCredentials(t, dir, e2eLogseqCreds); err != nil {
		t.Fatal(err)
	}

	// The config already points at the denying server, so Ping gets the 401.
	code, _, errOut := runCLI(t, conf, "check")
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
