package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReadCalendarFlagReplacesConfig(t *testing.T) {
	e := mustE2E(t, e2eOptions{
		Google:        true,
		ReadCalendars: []string{"a@example.com", "b@example.com"},
	})

	// No flag: write plus both config reads, in order.
	code, out, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("forecast exit = %d, stderr %q", code, errOut)
	}
	ids := e.Calendar.ReadIDs()
	if len(ids) != 3 {
		t.Fatalf("reads = %q, want write plus two config reads", ids)
	}
	if ids[1] != "a@example.com" || ids[2] != "b@example.com" {
		t.Fatalf("reads = %q, want config order", ids)
	}
	var view struct {
		Busy struct {
			WriteCalendar string `json:"write_calendar"`
			Sources       []struct {
				CalendarID string `json:"calendar_id"`
				Status     string `json:"status"`
			} `json:"sources"`
		} `json:"busy_sources"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(view.Busy.Sources) != 2 {
		t.Fatalf("busy_sources = %+v", view.Busy)
	}

	// One flag replaces the whole list.
	e.Calendar.ResetReads()
	code, _, errOut = e.Run("--json", "forecast", "--read-calendar", "c@example.com")
	if code != 0 {
		t.Fatalf("forecast with flag exit = %d, stderr %q", code, errOut)
	}
	if ids := e.Calendar.ReadIDs(); len(ids) != 2 || ids[1] != "c@example.com" {
		t.Fatalf("flag reads = %q, want write plus c only", ids)
	}

	// Repeatable: two flags, config order is flag order.
	e.Calendar.ResetReads()
	code, _, errOut = e.Run("--json", "forecast",
		"--read-calendar", "c@example.com", "--read-calendar", "d@example.com")
	if code != 0 {
		t.Fatalf("forecast with two flags exit = %d, stderr %q", code, errOut)
	}
	if ids := e.Calendar.ReadIDs(); len(ids) != 3 || ids[1] != "c@example.com" || ids[2] != "d@example.com" {
		t.Fatalf("two-flag reads = %q", ids)
	}

	// The write ID in the flag is silently dropped.
	e.Calendar.ResetReads()
	code, _, errOut = e.Run("--json", "forecast", "--read-calendar", "primary")
	if code != 0 {
		t.Fatalf("forecast with write-ID flag exit = %d, stderr %q", code, errOut)
	}
	if ids := e.Calendar.ReadIDs(); len(ids) != 1 {
		t.Fatalf("write-ID flag reads = %q, want write alone", ids)
	}
}

func TestForecastBusySourcesLineAndSkippedWarning(t *testing.T) {
	busyStart, busyEnd := fixtureBusyDay(t)
	good := fmt.Sprintf(`{"items": [{"id":"g","status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q}}]}`,
		busyStart.Format(time.RFC3339), busyEnd.Format(time.RFC3339))
	e := mustE2E(t, e2eOptions{
		Google:         true,
		ReadCalendars:  []string{"good@example.com", "bad@example.com"},
		CalendarReads:  map[string]string{"good@example.com": good},
		CalendarStatus: map[string]int{"bad@example.com": http.StatusForbidden},
	})

	code, out, errOut := e.Run("forecast")
	if code != 0 {
		t.Fatalf("forecast exit = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "busy sources:") || !strings.Contains(out, "primary") ||
		!strings.Contains(out, "good@example.com") || !strings.Contains(out, "bad@example.com skipped:403") {
		t.Fatalf("busy sources line missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "bad@example.com") ||
		!strings.Contains(out, "free/busy-only") {
		t.Fatalf("skipped warning missing:\n%s", out)
	}
}
