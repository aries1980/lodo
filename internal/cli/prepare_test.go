package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

// degradedCheckSetup is check's no-calendar path: a live graph and a
// credentials file with no Google grant.
func degradedCheckSetup(t *testing.T) string {
	t.Helper()
	conf, cred := writeTestSetup(t, miniApp(t))
	stripGoogle(t, cred)
	return conf
}

// calendarCheckSetup is check's calendar path: the same graph plus a fake
// Google with no events and a stored refresh token.
func calendarCheckSetup(t *testing.T) string {
	t.Helper()
	conf, _ := writeTestSetup(t, miniApp(t))
	base := (&fakeGoogle{}).mount(t, `{"items":[]}`)
	pointBoth(t, oauth2Endpoint(base), base)
	return conf
}

// checkPathCase is one of the two paths a read-only command can take.
type checkPathCase struct {
	name  string
	setup func(*testing.T) string
	// calendar reports whether the case exercises the calendar path.
	calendar bool
}

func checkPathCases() []checkPathCase {
	return []checkPathCase{
		{name: "without calendar", setup: degradedCheckSetup},
		{name: "with calendar", setup: calendarCheckSetup, calendar: true},
	}
}

func TestPrepareRejectsUnknownCommand(t *testing.T) {
	if _, err := prepare(t.Context(), &options{}, configOverrides{}, false, "nope"); err == nil {
		t.Fatal("a command with no declared needs must fail before any I/O")
	}
}

func TestCheckJSONBusyOnlyWithCalendar(t *testing.T) {
	for _, tc := range checkPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			conf := tc.setup(t)
			stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
			if code := Run([]string{"--config", conf, "--json", "check"}, stdout, stderr); code != 0 {
				t.Fatalf("exit = %d, stderr %q", code, stderr.String())
			}
			var view map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
			}
			if _, has := view["busy_sources"]; has != tc.calendar {
				t.Fatalf("busy_sources present = %v, want %v:\n%s", has, tc.calendar, stdout.String())
			}
		})
	}
}

func TestCheckStrictExitMessage(t *testing.T) {
	clean := run.CheckOutcome{}
	if err := checkStrictExit(clean, true); err != nil {
		t.Fatalf("a clean run must pass --strict, got %v", err)
	}

	issue := resolve.Issue{Kind: "k", Context: "c", Detail: "d"}
	orphan := run.Orphan{EventID: "e"}
	outcome := run.CheckOutcome{Scheduled: run.Scheduled{Issues: []resolve.Issue{issue}}, Orphans: []run.Orphan{orphan}}

	// Without a calendar, orphans cannot exist, so the message stays silent
	// about them (byte-compatible with the pre-scaffold fallback).
	err := checkStrictExit(outcome, false)
	if err == nil || strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("no-calendar strict error = %v, want one naming no orphans", err)
	}
	// With a calendar, the same outcome names the orphan and its count.
	err = checkStrictExit(outcome, true)
	if err == nil || !strings.Contains(err.Error(), ", 1 orphaned") {
		t.Fatalf("calendar strict error = %v, want the orphan count", err)
	}
	if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != codeValidation {
		t.Fatalf("strict error = %#v, want exit code %d", err, codeValidation)
	}
}

// With a calendar, an orphaned Managed event is a validation failure: check
// --strict counts it and names it.
func TestCheckStrictCountsOrphansWithCalendar(t *testing.T) {
	conf, _ := writeTestSetup(t, miniApp(t))
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	events := fmt.Sprintf(`{"items":[{"id":"lodo-orphan","status":"confirmed","summary":"ghost","start":{"dateTime":%q},"end":{"dateTime":%q},"extendedProperties":{"private":{"lodo":"true"}}}]}`,
		start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	base := (&fakeGoogle{}).mount(t, events)
	pointBoth(t, oauth2Endpoint(base), base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "check", "--strict"}, stdout, stderr); code != 2 {
		t.Fatalf("exit = %d, want 2 for an orphaned event (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), ", 1 orphaned") {
		t.Fatalf("stderr must count the orphan: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Orphaned: 1") {
		t.Fatalf("the report must list the orphan:\n%s", stdout.String())
	}
}

// A degraded check starts once: the fallback used to log a second run.start.
func TestDegradedCheckLogsOneRunStart(t *testing.T) {
	conf := degradedCheckSetup(t)
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--log-format=json", "check"}, stdout, stderr); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	if n := strings.Count(stderr.String(), `"event":"run.start"`); n != 1 {
		t.Fatalf("run.start records = %d, want 1: %q", n, stderr.String())
	}
}

// refresh warns about shared-calendar conflicts in JSON log mode too; the
// record used to sit in the text-only branch, where logRunWarn is a no-op.
func TestRefreshJSONLogsConflicts(t *testing.T) {
	api := miniApp(t)
	conf, _ := writeSetupWithReads(t, api, "  read_calendars:\n    - shared@example.com\n")
	start := time.Now().Add(30 * time.Hour).Truncate(time.Hour)
	end := start.Add(time.Hour)
	span := fmt.Sprintf(`"start":{"dateTime":%q},"end":{"dateTime":%q}`, start.Format(time.RFC3339), end.Format(time.RFC3339))
	f := &calendarFake{events: map[string]string{
		"primary":            fmt.Sprintf(`{"items":[{"id":"lodo-managed","status":"confirmed","summary":"Managed",%s,"extendedProperties":{"private":{"lodo":"true"}}}]}`, span),
		"shared@example.com": fmt.Sprintf(`{"items":[{"id":"shared-busy","status":"confirmed",%s}]}`, span),
	}}
	base := f.mount(t)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--json", "--log-format=json", "refresh"}, stdout, stderr); code != 0 {
		t.Fatalf("refresh exit = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"event":"refresh.conflict"`) {
		t.Fatalf("stderr must carry the conflict record: %q", stderr.String())
	}
}
