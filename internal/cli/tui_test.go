package cli

import (
	"strings"
	"testing"
)

func TestBareLodoShowsHelpWhenNonTTY(t *testing.T) {
	old := ttyCheck
	ttyCheck = func() bool { return false }
	t.Cleanup(func() { ttyCheck = old })
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := Run([]string{}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for help, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Schedule Logseq tasks") {
		t.Fatalf("help missing short:\n%s", stdout.String())
	}
}

func TestBareLodoJSONShowsHelpNotTUI(t *testing.T) {
	old := ttyCheck
	ttyCheck = func() bool { return true }
	t.Cleanup(func() { ttyCheck = old })
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := Run([]string{"--json"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for help, stderr %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Tasks") && strings.Contains(stdout.String(), "Duration") {
		t.Fatalf("TUI table must not render for --json:\n%s", stdout.String())
	}
}

func TestBareLodoTTYRoutesToTUIBeforeHang(t *testing.T) {
	old := ttyCheck
	ttyCheck = func() bool { return true }
	t.Cleanup(func() { ttyCheck = old })
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := Run([]string{"--config", "/nonexistent/lodo-config.yaml"}, stdout, stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for missing config via TUI path", code)
	}
	if !strings.Contains(stderr.String(), "config file") {
		t.Fatalf("stderr must name the config: %q", stderr.String())
	}
}
