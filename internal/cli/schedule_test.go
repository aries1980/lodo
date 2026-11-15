package cli

import (
	"encoding/json"
	"testing"

	"github.com/aries1980/lodo/internal/wire"
)

func TestScheduleWritesThenNoOp(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true})

	code, out, errOut := e.Run("--json", "schedule")
	if code != 0 {
		t.Fatalf("first schedule exit = %d, stderr %q", code, errOut)
	}
	var first struct {
		SchemaVersion int `json:"schema_version"`
		Blocks        []struct {
			TaskID string `json:"task_id"`
		} `json:"blocks"`
		Writes []struct {
			Op string `json:"op"`
		} `json:"writes"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("first schedule JSON: %v\n%s", err, out)
	}
	if first.SchemaVersion != wire.ScheduleVersion {
		t.Fatalf("schema_version = %d, want %d", first.SchemaVersion, wire.ScheduleVersion)
	}
	if len(first.Blocks) == 0 || len(first.Writes) == 0 {
		t.Fatalf("first schedule must place and write: blocks=%d writes=%d", len(first.Blocks), len(first.Writes))
	}
	if got := e.Calendar.WriteCount(); got == 0 {
		t.Fatalf("fake saw no mutating calls")
	}

	// Second run: no-op, zero writes.
	e.Calendar.ResetWrites()
	code, next, errOut := e.Run("--json", "schedule")
	if code != 0 {
		t.Fatalf("second schedule exit = %d, stderr %q", code, errOut)
	}
	var second struct {
		Writes []any `json:"writes"`
	}
	if err := json.Unmarshal([]byte(next), &second); err != nil {
		t.Fatalf("second schedule JSON: %v", err)
	}
	if len(second.Writes) != 0 {
		t.Fatalf("second schedule writes = %d, want 0 (no-op)", len(second.Writes))
	}
	if got := e.Calendar.WriteCount(); got != 0 {
		t.Fatalf("second run issued %d calendar writes, want 0", got)
	}

	// Third no-op run is byte-identical to the second: --json determinism.
	code, third, errOut := e.Run("--json", "schedule")
	if code != 0 {
		t.Fatalf("third schedule exit = %d, stderr %q", code, errOut)
	}
	if next != third {
		t.Fatalf("no-op schedules differ:\n%s\n---\n%s", next, third)
	}
}

func TestScheduleDryRunEqualsForecast(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true})

	code, forecast, errOut := e.Run("--json", "forecast")
	if code != 0 {
		t.Fatalf("forecast exit = %d, stderr %q", code, errOut)
	}
	code, dry, errOut := e.Run("--json", "schedule", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit = %d, stderr %q", code, errOut)
	}
	if forecast != dry {
		t.Fatalf("--dry-run must equal forecast:\n%s\n---\n%s", forecast, dry)
	}
	if got := e.Calendar.WriteCount(); got != 0 {
		t.Fatalf("dry-run issued %d writes, want 0", got)
	}
}

func TestRefreshNeverFetchesTheGraph(t *testing.T) {
	e := mustE2E(t, e2eOptions{Google: true})

	// Point Logseq at a gone server after setup: refresh must still succeed
	// because it never resolves the graph.
	if _, err := writeConfig(t, e.Dir, "http://127.0.0.1:1", ""); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := e.Run("--json", "refresh")
	if code != 0 {
		t.Fatalf("refresh with dead graph exit = %d, stderr %q (must not fetch the graph)", code, errOut)
	}
	var view struct {
		SchemaVersion int   `json:"schema_version"`
		Dispositions  []any `json:"dispositions"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("refresh JSON: %v", err)
	}
	if view.SchemaVersion != wire.RefreshVersion {
		t.Fatalf("schema_version = %d, want %d", view.SchemaVersion, wire.RefreshVersion)
	}
	// Determinism: a second refresh is byte-identical.
	code, again, errOut := e.Run("--json", "refresh")
	if code != 0 {
		t.Fatalf("second refresh exit = %d, stderr %q", code, errOut)
	}
	if out != again {
		t.Fatalf("refresh JSON not deterministic:\n%s\n---\n%s", out, again)
	}
}

func TestExitCodeMatrix(t *testing.T) {
	// Schedule without google creds: exit 1 (operational). The endpoints
	// point at a gone server so a network attempt would also fail fast; the
	// missing grant must fail before any request.
	e := mustE2E(t, e2eOptions{})
	pointGoogle(t, oauth2Endpoint("http://127.0.0.1:1"), "http://127.0.0.1:1")

	if code, _, _ := e.Run("schedule"); code != 1 {
		t.Fatalf("schedule without google exit = %d, want 1", code)
	}
	if code, _, _ := e.Run("refresh"); code != 1 {
		t.Fatalf("refresh without google exit = %d, want 1", code)
	}

	// Orphaned event → schedule --strict exits 2, plain exits 0.
	o := mustE2E(t, e2eOptions{Google: true, CalendarEvents: []map[string]any{
		calendarEvent("lodo-orphan", "ghost", e2eAt(2, 9, 0), e2eAt(2, 9, 30), true),
	}})
	if code, _, _ := o.Run("schedule", "--strict"); code != 2 {
		t.Fatalf("schedule --strict with orphan exit = %d, want 2", code)
	}
	if code, _, _ := o.Run("schedule"); code != 0 {
		t.Fatalf("schedule with orphan exit = %d, want 0 (reported, not failed)", code)
	}
}
