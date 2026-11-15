package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/aries1980/lodo/internal/wire"
)

// fixtureBusyDay names the weekday morning the busy event spans: the Friday
// two days after the pinned fixture clock, 09:00-12:00 London. Anchored to
// the same instant the CLI runs at, so the test never drifts out of the
// window and never races a midnight or a DST change.
func fixtureBusyDay(t *testing.T) (start, end time.Time) {
	t.Helper()
	return e2eAt(2, 9, 0), e2eAt(2, 12, 0)
}

func TestForecastEndToEndWithFakeGoogle(t *testing.T) {
	busyStart, busyEnd := fixtureBusyDay(t)

	// One external event spanning the busy stretch, and one Managed event
	// Lodo itself owns (its span deliberate: inside the same window).
	e := mustE2E(t, e2eOptions{Google: true, CalendarEvents: []map[string]any{
		calendarEvent("ext-meeting", "External meeting", busyStart, busyEnd, false),
		calendarEvent("lodo-is-mine", "A Block", busyEnd, busyEnd.Add(2*time.Hour), true),
	}})

	code, out, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errOut)
	}

	var view struct {
		SchemaVersion int `json:"schema_version"`
		Blocks        []struct {
			TaskID string    `json:"task_id"`
			Start  time.Time `json:"start"`
			End    time.Time `json:"end"`
		} `json:"blocks"`
		Managed []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"managed_left_out"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, out)
	}
	if view.SchemaVersion != wire.ForecastVersion {
		t.Fatalf("schema_version = %d, want %d", view.SchemaVersion, wire.ForecastVersion)
	}

	// The client refreshed exactly once, with the stored refresh token.
	if got := e.Calendar.Refreshes(); got != 1 {
		t.Fatalf("refresh grants = %d, want 1", got)
	}
	if tokens := e.Calendar.TokenSeen(); len(tokens) != 1 || tokens[0] != "1//stored-refresh-token" {
		t.Fatalf("refresh tokens = %v", tokens)
	}
	if got := e.Calendar.Lists(); got != 1 {
		t.Fatalf("events.list calls = %d, want 1 (an unfiltered window fits one page)", got)
	}

	// Busy shaped the plan (a block exists), the Managed event travelled
	// back as the note it keeps until schedule folds it in.
	if len(view.Blocks) == 0 {
		t.Fatal("no blocks: nothing should prevent the 30m Task's placement")
	}
	if len(view.Managed) != 1 || view.Managed[0].ID != "lodo-is-mine" || view.Managed[0].Summary != "A Block" {
		t.Fatalf("managed_left_out = %+v", view.Managed)
	}
	loc := busyStart.Location()
	for _, b := range view.Blocks {
		start, end := b.Start.In(loc), b.End.In(loc)
		if start.Before(busyEnd) && end.After(busyStart) {
			t.Fatalf("block %s..%s overlaps busy %s..%s", start, end, busyStart, busyEnd)
		}
	}

	// Same inputs, one run apart: byte-identical output.
	code, next, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("second run: exit = %d, stderr %q", code, errOut)
	}
	if out != next {
		t.Fatalf("a second forecast moved something:\n%s\n---\n%s", out, next)
	}
}

// The text rendering notes the Managed hole M4 leaves, in the same voice
// the JSON does.
func TestForecastTextNotesTheManagedHole(t *testing.T) {
	busyStart, busyEnd := fixtureBusyDay(t)
	e := mustE2E(t, e2eOptions{Google: true, CalendarEvents: []map[string]any{
		calendarEvent("lodo-is-mine", "A Block", busyStart, busyEnd, true),
	}})

	code, out, errOut := e.Run("forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		`Schedule that would be written to calendar "primary"`,
		"not counted as busy yet",
		"lodo-is-mine — A Block",
		"Write the milestone note",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
	// Explanations only with --verbose; a shortfall would assert against it:
	// the 30m Task fits, so none exists.
	if strings.Contains(out, "Shortfall:") || strings.Contains(out, "Explanations:") {
		t.Fatalf("plain forecast must stay plain:\n%s", out)
	}

	code, out, errOut = e.Run("--verbose", "forecast")
	if code != 0 {
		t.Fatalf("--verbose: exit = %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "Explanations:") {
		t.Fatalf("--verbose must add explanations:\n%s", out)
	}
}

func TestForecastCalendarFailureIsOperational(t *testing.T) {
	// An endpoint whose server has gone away: the refresh cannot fetch a
	// token, so the forecast refuses rather than plan against a silently
	// free calendar.
	gone := deadBase

	dir := t.TempDir()
	conf, err := writeConfig(t, dir, gone, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeCredentials(t, dir, e2eGoogleCreds); err != nil {
		t.Fatal(err)
	}
	pointGoogle(t, oauth2Endpoint(gone), gone)

	code, out, errOut := runCLI(t, conf, "--json", "forecast")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stdout %q)", code, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a failed calendar prints no schedule: %q", out)
	}
	if strings.TrimSpace(errOut) == "" {
		t.Fatal("stderr must say why")
	}
}
