package run_test

import (
	"context"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/run"
)

// conflictCal simulates the Google 409/404 races: the List window missed an
// event that still lives on the calendar (e.g. it sits in the past), so an
// insert collides, or a patch target vanished between List and write.
type conflictCal struct {
	mask     grid.Mask
	managed  []calendar.Event
	report   calendar.BusyReport
	inserts  []string
	patches  []string
	insertFn func(id string) error
	patchFn  func(id string) error
}

func (c *conflictCal) Busy(_ context.Context, _ *grid.Grid, _ time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error) {
	return c.mask, c.managed, c.report, nil
}

func (c *conflictCal) List(_ context.Context, _, _ time.Time) ([]calendar.Event, error) {
	return c.managed, nil
}

func (c *conflictCal) Insert(_ context.Context, e calendar.Event) (calendar.Event, error) {
	c.inserts = append(c.inserts, e.ID)
	if c.insertFn != nil {
		if err := c.insertFn(e.ID); err != nil {
			return calendar.Event{}, err
		}
	}
	return e, nil
}

func (c *conflictCal) Patch(_ context.Context, id string, _ calendar.Event) (calendar.Event, error) {
	c.patches = append(c.patches, id)
	if c.patchFn != nil {
		if err := c.patchFn(id); err != nil {
			return calendar.Event{}, err
		}
	}
	return calendar.Event{ID: id}, nil
}

func (c *conflictCal) Delete(_ context.Context, _ string) error { return nil }

func duplicateIDError() error {
	return &calendar.APIError{StatusCode: 409, Message: "The requested identifier already exists.", Path: "calendars/primary/events"}
}

func missingError() error {
	return &calendar.APIError{StatusCode: 404, Message: "Resource has been deleted", Path: "calendars/primary/events/e"}
}

func TestInsertConflictFallsBackToPatch(t *testing.T) {
	cfg := testConfig()
	now := at(9, 8, 0)
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)
	cal := &conflictCal{
		mask:   grid.NewMask(g.Len()),
		report: calendar.BusyReport{WriteCalendar: "primary", Shared: grid.NewMask(g.Len())},
		insertFn: func(string) error {
			// The event lives on the calendar outside the list window.
			return duplicateIDError()
		},
	}
	st := openTestStore(t)

	outcome, err := run.Schedule(context.Background(), newStubClient(busyGraph()), cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Schedule with 409 insert: %v", err)
	}
	if len(outcome.Writes) == 0 {
		t.Fatalf("want the planned insert in Writes, got none")
	}
	eventID := reconcile.EventID("t-audit", 0)
	if len(cal.patches) != 1 || cal.patches[0] != eventID {
		t.Fatalf("patches = %v, want one patch of %s after the 409", cal.patches, eventID)
	}
}

func TestPatchMissingFallsBackToInsert(t *testing.T) {
	cfg := testConfig()
	now := at(9, 8, 0)
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)
	eventID := reconcile.EventID("t-audit", 0)
	// Managed event seen by List at stale times: diffPlan wants a patch, but
	// the event vanished before the write.
	stale := calendar.Event{
		ID: eventID, Summary: "Audit the ledger",
		Start: calendar.EventTime{DateTime: at(9, 10, 0).Format(time.RFC3339)},
		End:   calendar.EventTime{DateTime: at(9, 10, 30).Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": "t-audit", "block": "0",
		}},
	}
	cal := &conflictCal{
		mask:    grid.NewMask(g.Len()),
		managed: []calendar.Event{stale},
		report:  calendar.BusyReport{WriteCalendar: "primary", Shared: grid.NewMask(g.Len())},
		patchFn: func(string) error { return missingError() },
	}
	st := openTestStore(t)

	if _, err := run.Schedule(context.Background(), newStubClient(busyGraph()), cal, st, cfg, now); err != nil {
		t.Fatalf("Schedule with 404 patch: %v", err)
	}
	if len(cal.inserts) != 1 || cal.inserts[0] != eventID {
		t.Fatalf("inserts = %v, want one insert of %s after the 404", cal.inserts, eventID)
	}
}
