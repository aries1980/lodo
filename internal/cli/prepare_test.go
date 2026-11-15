package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

// degradedCheckSetup is check's no-calendar path: a live graph and a
// credentials file with no Google grant.
func degradedCheckSetup(t *testing.T) string {
	t.Helper()
	return mustE2E(t, e2eOptions{}).Conf
}

// calendarCheckSetup is check's calendar path: the same graph plus a fake
// Google with no events and a stored refresh token.
func calendarCheckSetup(t *testing.T) string {
	t.Helper()
	return mustE2E(t, e2eOptions{Google: true}).Conf
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
	e := mustE2E(t, e2eOptions{Google: true, CalendarEvents: []map[string]any{
		calendarEvent("lodo-orphan", "ghost", e2eAt(2, 9, 0), e2eAt(2, 10, 0), true),
	}})

	code, out, errOut := e.Run("check", "--strict")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for an orphaned event (stderr %q)", code, errOut)
	}
	if !strings.Contains(errOut, ", 1 orphaned") {
		t.Fatalf("stderr must count the orphan: %q", errOut)
	}
	if !strings.Contains(out, "Orphaned: 1") {
		t.Fatalf("the report must list the orphan:\n%s", out)
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
	start, end := e2eAt(1, 9, 0), e2eAt(1, 10, 0)
	span := fmt.Sprintf(`"start":{"dateTime":%q},"end":{"dateTime":%q}`,
		start.Format(time.RFC3339), end.Format(time.RFC3339))
	e := mustE2E(t, e2eOptions{
		Google:        true,
		ReadCalendars: []string{"shared@example.com"},
		CalendarReads: map[string]string{
			"primary":            fmt.Sprintf(`{"items":[{"id":"lodo-managed","status":"confirmed","summary":"Managed",%s,"extendedProperties":{"private":{"lodo":"true"}}}]}`, span),
			"shared@example.com": fmt.Sprintf(`{"items":[{"id":"shared-busy","status":"confirmed",%s}]}`, span),
		},
	})

	code, _, errOut := e.Run("--json", "--log-format=json", "refresh")
	if code != 0 {
		t.Fatalf("refresh exit = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(errOut, `"event":"refresh.conflict"`) {
		t.Fatalf("stderr must carry the conflict record: %q", errOut)
	}
}
