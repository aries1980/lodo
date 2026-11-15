package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aries1980/lodo/internal/wire"
)

// An invalid ui.theme must warn on stderr and continue with the concrete
// default: a bad theme never fails a run.
func TestInvalidThemeNameWarnsAndContinues(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true, ConfigExtra: "ui:\n  theme: no-such-theme\n"})

	code, out, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with an invalid theme (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, `ui.theme "no-such-theme": unknown theme; falling back to concrete`) {
		t.Fatalf("stderr must name the bad theme and the fallback, got %q", errOut)
	}
	var view wire.Forecast
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, out)
	}
	if len(view.Blocks) == 0 {
		t.Fatalf("no blocks: the run must continue with the default theme:\n%s", out)
	}
}

// A theme path that cannot be read warns the same way and continues.
func TestMissingThemeFileWarnsAndContinues(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true, ConfigExtra: "ui:\n  theme: /nonexistent/lodo-theme.yaml\n"})

	code, out, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with a missing theme file (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, `ui.theme "/nonexistent/lodo-theme.yaml"`) || !strings.Contains(errOut, "falling back to concrete") {
		t.Fatalf("stderr must name the bad theme file and the fallback, got %q", errOut)
	}
	var view wire.Forecast
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, out)
	}
	if len(view.Blocks) == 0 {
		t.Fatalf("no blocks: the run must continue with the default theme:\n%s", out)
	}
}

// A valid theme resolves silently: no fallback warning.
func TestValidThemeEmitsNoWarning(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true, ConfigExtra: "ui:\n  theme: monokai\n"})

	code, out, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errOut)
	}
	if strings.Contains(errOut, "falling back to concrete") {
		t.Fatalf("a valid theme must not warn, got %q", errOut)
	}
	var view wire.Forecast
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout must stay the JSON contract: %v\n%s", err, out)
	}
}

// The no-calendar check fallback reuses the caller's resolved theme, so one
// invalid ui.theme warns exactly once per run.
func TestCheckDegradedModeWarnsOnceForBadTheme(t *testing.T) {
	e := mustE2E(t, e2eOptions{ConfigExtra: "ui:\n  theme: no-such-theme\n"})

	code, out, errOut := e.Run("check")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with an invalid theme (stderr %q)", code, errOut)
	}
	if n := strings.Count(errOut, "falling back to concrete"); n != 1 {
		t.Fatalf("want exactly one theme warning, got %d in %q", n, errOut)
	}
	if !strings.Contains(out, "Summary:") {
		t.Fatalf("the run must continue past the bad theme:\n%s", out)
	}
}
