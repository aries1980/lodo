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

// explainNow runs explain against the minimal graph and returns the pinned
// instant the payload reports. The credentials carry no Google grant, so the
// command degrades like check and never touches the network.
func explainNow(t *testing.T, args ...string) time.Time {
	t.Helper()
	conf, cred := writeTestSetup(t, miniApp(t))
	stripGoogle(t, cred)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run(append([]string{"--config", conf, "--json", "explain", "t-task-uuid"}, args...), stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	var view wire.Explain
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout.String())
	}
	return view.Context.Now
}

func TestNowFlagPinsTheClock(t *testing.T) {
	want := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if got := explainNow(t, "--now", want.Format(time.RFC3339)); !got.Equal(want) {
		t.Fatalf("context.now = %v, want %v", got, want)
	}
}

func TestNowFromEnv(t *testing.T) {
	want := time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC)
	t.Setenv("LODO_NOW", want.Format(time.RFC3339))
	if got := explainNow(t); !got.Equal(want) {
		t.Fatalf("context.now = %v, want the LODO_NOW instant %v", got, want)
	}
}

func TestNowFlagBeatsEnv(t *testing.T) {
	fromEnv := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	fromFlag := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	t.Setenv("LODO_NOW", fromEnv.Format(time.RFC3339))
	if got := explainNow(t, "--now", fromFlag.Format(time.RFC3339)); !got.Equal(fromFlag) {
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
