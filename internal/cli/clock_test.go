package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/wire"
)

// explainNow runs explain against the standard fixture and returns the
// pinned instant the payload reports. env is applied after the fixture is
// written so a case can override its LODO_NOW.
func explainNow(t *testing.T, env map[string]string, args ...string) time.Time {
	t.Helper()
	e := mustE2E(t, e2eOptions{})
	for k, v := range env {
		t.Setenv(k, v)
	}

	code, out, errOut := e.Run(append([]string{"--json", "explain", "t-task-uuid"}, args...)...)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errOut)
	}
	var view wire.Explain
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, out)
	}
	return view.Context.Now
}

func TestNowFlagPinsTheClock(t *testing.T) {
	want := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if got := explainNow(t, nil, "--now", want.Format(time.RFC3339)); !got.Equal(want) {
		t.Fatalf("context.now = %v, want %v", got, want)
	}
}

func TestNowFromEnv(t *testing.T) {
	want := time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC)
	if got := explainNow(t, map[string]string{"LODO_NOW": want.Format(time.RFC3339)}); !got.Equal(want) {
		t.Fatalf("context.now = %v, want the LODO_NOW instant %v", got, want)
	}
}

func TestNowFlagBeatsEnv(t *testing.T) {
	fromEnv := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	fromFlag := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	env := map[string]string{"LODO_NOW": fromEnv.Format(time.RFC3339)}
	if got := explainNow(t, env, "--now", fromFlag.Format(time.RFC3339)); !got.Equal(fromFlag) {
		t.Fatalf("context.now = %v, want the flag instant %v", got, fromFlag)
	}
}

func TestInvalidNowFailsBeforeConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent", "config.yaml")
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", missing, "--now", "yesterday", "check"}, stdout, stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a malformed --now", code)
	}
	if !strings.Contains(stderr.String(), "--now") {
		t.Fatalf("stderr must name the flag: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "config file") {
		t.Fatalf("--now must fail before the config read: %q", stderr.String())
	}
}

func TestNowFlagIsHidden(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--help"}, stdout, stderr); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "--now") {
		t.Fatalf("--now is a testing aid and must stay out of help:\n%s", stdout.String())
	}
}

// TestCommandsAtPinnedClock runs the command surface at a midweek instant, a
// Sunday, and both DST transitions. Every fixture date derives from the pinned
// clock, so no command may care which day it is.
func TestCommandsAtPinnedClock(t *testing.T) {
	clocks := []struct {
		name string
		now  time.Time
	}{
		{"midweek", time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)},
		{"sunday", time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)},
		// 2026-03-29 01:00 UTC: Europe/London springs forward.
		{"spring forward", time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC)},
		// 2026-10-25 01:00 UTC: Europe/London falls back into the repeated hour.
		{"fall back", time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)},
	}

	readOnly := [][]string{
		{"--json", "check"},
		{"--json", "forecast"},
		{"--json", "refresh"},
		{"--json", "explain", "t-task-uuid"},
	}
	for _, tc := range clocks {
		t.Run(tc.name, func(t *testing.T) {
			e := mustE2E(t, e2eOptions{Google: true, Now: tc.now})

			for _, args := range readOnly {
				code, out, errOut := e.Run(args...)
				if code != 0 {
					t.Fatalf("%v exit = %d, stderr %q", args, code, errOut)
				}
				if !json.Valid([]byte(out)) {
					t.Fatalf("%v stdout is not JSON:\n%s", args, out)
				}
			}

			// explain reports the pinned instant, not the wall clock.
			code, out, errOut := e.Run("--json", "explain", "t-task-uuid")
			if code != 0 {
				t.Fatalf("explain exit = %d, stderr %q", code, errOut)
			}
			var view wire.Explain
			if err := json.Unmarshal([]byte(out), &view); err != nil {
				t.Fatalf("explain JSON: %v\n%s", err, out)
			}
			if !view.Context.Now.Equal(tc.now) {
				t.Fatalf("context.now = %v, want %v", view.Context.Now, tc.now)
			}

			// schedule writes, and its second run is a no-op at any clock.
			if code, _, errOut := e.Run("--json", "schedule"); code != 0 {
				t.Fatalf("schedule exit = %d, stderr %q", code, errOut)
			}
			e.Calendar.ResetWrites()
			if code, _, errOut := e.Run("--json", "schedule"); code != 0 {
				t.Fatalf("second schedule exit = %d, stderr %q", code, errOut)
			}
			if got := e.Calendar.WriteCount(); got != 0 {
				t.Fatalf("second schedule issued %d writes, want 0", got)
			}
		})
	}
}
