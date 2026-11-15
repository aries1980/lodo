package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aries1980/lodo/internal/wire"
)

// appendTheme adds a ui.theme setting to a config file written by
// writeTestSetup, which carries no ui section of its own.
func appendTheme(t *testing.T, conf, value string) {
	t.Helper()
	raw, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("ui:\n  theme: "+value+"\n")...)
	if err := os.WriteFile(conf, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// An invalid ui.theme must warn on stderr and continue with the concrete
// default: a bad theme never fails a run.
func TestInvalidThemeNameWarnsAndContinues(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	appendTheme(t, conf, "no-such-theme")
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2Endpoint(base), base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with an invalid theme (stderr %q)", code, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, `ui.theme "no-such-theme": unknown theme; falling back to concrete`) {
		t.Fatalf("stderr must name the bad theme and the fallback, got %q", got)
	}
	var view wire.Forecast
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, stdout.String())
	}
	if len(view.Blocks) == 0 {
		t.Fatalf("no blocks: the run must continue with the default theme:\n%s", stdout.String())
	}
}

// A theme path that cannot be read warns the same way and continues.
func TestMissingThemeFileWarnsAndContinues(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	appendTheme(t, conf, "/nonexistent/lodo-theme.yaml")
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2Endpoint(base), base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with a missing theme file (stderr %q)", code, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, `ui.theme "/nonexistent/lodo-theme.yaml"`) || !strings.Contains(got, "falling back to concrete") {
		t.Fatalf("stderr must name the bad theme file and the fallback, got %q", got)
	}
	var view wire.Forecast
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, stdout.String())
	}
	if len(view.Blocks) == 0 {
		t.Fatalf("no blocks: the run must continue with the default theme:\n%s", stdout.String())
	}
}

// A valid theme resolves silently: no fallback warning.
func TestValidThemeEmitsNoWarning(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	appendTheme(t, conf, "monokai")
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2Endpoint(base), base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	if got := stderr.String(); strings.Contains(got, "falling back to concrete") {
		t.Fatalf("a valid theme must not warn, got %q", got)
	}
	var view wire.Forecast
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, stdout.String())
	}
}

// The no-calendar check fallback reuses the caller's resolved theme, so one
// invalid ui.theme warns exactly once per run.
func TestCheckDegradedModeWarnsOnceForBadTheme(t *testing.T) {
	api := miniApp(t)
	conf, cred := writeTestSetup(t, api)
	appendTheme(t, conf, "no-such-theme")
	// Drop the google section so check degrades to the no-calendar path.
	if err := os.WriteFile(cred, []byte(`{"logseq":{"token":"st-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "check"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with an invalid theme (stderr %q)", code, stderr.String())
	}
	if n := strings.Count(stderr.String(), "falling back to concrete"); n != 1 {
		t.Fatalf("want exactly one theme warning, got %d in %q", n, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Summary:") {
		t.Fatalf("the run must continue past the bad theme:\n%s", stdout.String())
	}
}
