package run_test

import (
	"context"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/run"
	"github.com/aries1980/lodo/internal/store"
)

// stubReader is a CalendarReader with canned busy output for refresh
// overlap tests: the mask, the write calendar's managed events, and the
// shared-only union.
type stubReader struct {
	mask    grid.Mask
	managed []calendar.Event
	report  calendar.BusyReport
	err     error
}

func (s stubReader) Busy(context.Context, *grid.Grid, time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error) {
	return s.mask, s.managed, s.report, s.err
}

func (s stubReader) List(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	return s.managed, nil
}

func TestRefreshReportsSharedOverlapAndChangesNothing(t *testing.T) {
	cfg := testConfig()
	now := at(9, 8, 0)
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)

	// The Managed Block Lodo owns: t-audit/0 at Monday 09:00-09:30, with a
	// matching store row so reconcile sees nothing to do.
	taskID := "t-audit"
	eventID := reconcile.EventID(taskID, 0)
	start, end := at(9, 9, 0), at(9, 9, 30)
	managed := []calendar.Event{{
		ID: eventID, Summary: "Audit the ledger",
		Start: calendar.EventTime{DateTime: start.Format(time.RFC3339)},
		End:   calendar.EventTime{DateTime: end.Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": taskID, "block": "0",
		}},
	}}

	// Shared busy time overlaps the Managed Block: 09:00-10:00 Monday.
	shared := g.Overlapping(at(9, 9, 0), at(9, 10, 0))
	union := grid.NewMask(g.Len()) // write busy is empty; union is shared here
	union.OrInto(shared)
	report := calendar.BusyReport{
		WriteCalendar: "primary", WriteEvents: 1,
		Sources: []calendar.SourceStatus{{CalendarID: "partner@example.com", Status: calendar.SourceRead, Events: 1}},
		Shared:  shared,
	}

	st := openTestStore(t)
	if err := st.Commit(
		[]store.Binding{{TaskID: taskID, BlockIndex: 0, EventID: eventID, Start: start, End: end, Slots: 2}},
		nil, nil, nil,
	); err != nil {
		t.Fatalf("seed Commit: %v", err)
	}

	outcome, err := run.Refresh(context.Background(), stubReader{mask: union, managed: managed, report: report}, st, cfg, now)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(outcome.Conflicts) != 1 || outcome.Conflicts[0].EventID != eventID || outcome.Conflicts[0].TaskID != taskID {
		t.Fatalf("conflicts = %+v, want the overlapping Managed Block", outcome.Conflicts)
	}
	if len(outcome.Adopted) != 0 || len(outcome.Captured) != 0 {
		t.Fatalf("adopted=%+v captured=%+v, want nothing moved or stood down", outcome.Adopted, outcome.Captured)
	}
	bindings, _, _, err := st.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(bindings) != 1 || bindings[0].EventID != eventID {
		t.Fatalf("bindings = %+v, refresh must change nothing", bindings)
	}
	if len(outcome.Busy.Sources) != 1 || outcome.Busy.Sources[0].Status != calendar.SourceRead {
		t.Fatalf("busy = %+v", outcome.Busy)
	}
}

func TestRefreshNoConflictWhenDisjoint(t *testing.T) {
	cfg := testConfig()
	now := at(9, 8, 0)
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)

	taskID := "t-audit"
	eventID := reconcile.EventID(taskID, 0)
	start, end := at(9, 9, 0), at(9, 9, 30)
	managed := []calendar.Event{{
		ID: eventID, Summary: "Audit the ledger",
		Start: calendar.EventTime{DateTime: start.Format(time.RFC3339)},
		End:   calendar.EventTime{DateTime: end.Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": taskID, "block": "0",
		}},
	}}

	// Shared busy far from the Managed Block: no conflict.
	shared := g.Overlapping(at(9, 14, 0), at(9, 15, 0))
	report := calendar.BusyReport{
		WriteCalendar: "primary", WriteEvents: 1,
		Sources: []calendar.SourceStatus{{CalendarID: "partner@example.com", Status: calendar.SourceRead, Events: 1}},
		Shared:  shared,
	}

	st := openTestStore(t)
	if err := st.Commit(
		[]store.Binding{{TaskID: taskID, BlockIndex: 0, EventID: eventID, Start: start, End: end, Slots: 2}},
		nil, nil, nil,
	); err != nil {
		t.Fatalf("seed Commit: %v", err)
	}

	outcome, err := run.Refresh(context.Background(), stubReader{mask: shared, managed: managed, report: report}, st, cfg, now)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(outcome.Conflicts) != 0 {
		t.Fatalf("conflicts = %+v, want none when disjoint", outcome.Conflicts)
	}
}
