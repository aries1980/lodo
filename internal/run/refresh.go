// Refresh path: pull-only, never recomputing. Reconciles in memory, commits
// bindings/dispositions, reports shared-calendar conflicts. Never resolves
// the graph, never writes the calendar.
package run

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/store"
)

type Conflict struct {
	TaskID     string
	BlockIndex int // -1 when the private pointer is unusable
	EventID    string
	Summary    string
}

// RefreshOutcome is one refresh run: pull-only, never recomputing.
type RefreshOutcome struct {
	Dispositions []model.Disposition // all stored after commit, ordered
	Adopted      []store.Binding     // binding rows adopted/updated this run
	Captured     []model.Disposition // disposition rows recorded this run
	// Busy carries the write calendar plus per-read-calendar status.
	Busy calendar.BusyReport
	// Conflicts are Managed events overlapping shared busy time. No moves,
	// no stand-downs; re-placement happens only in schedule.
	Conflicts []Conflict
}

// Refresh pulls the calendar into local state without recomputing: reconcile in
// memory, then commit bindings/dispositions (no snapshots) and run audit. It
// never resolves the graph and never writes the calendar, so it cannot fabricate
// intent. Shared-calendar conflicts are reported but not acted on.
func Refresh(ctx context.Context, cal CalendarReader, runStore Store, cfg config.Config, now time.Time) (RefreshOutcome, error) {
	state, err := loadCalendarState(ctx, cal, runStore, cfg, now)
	if err != nil {
		return RefreshOutcome{}, err
	}
	// Refresh never writes snapshots: it cannot resolve the graph.
	if err := runStore.Commit(state.reconciliation.BindingUpserts, state.reconciliation.BindingDeletes, state.reconciliation.DispositionUpserts, nil); err != nil {
		return RefreshOutcome{}, err
	}
	if err := recordRun(runStore, "refresh", state.busyReport, now); err != nil {
		return RefreshOutcome{}, err
	}
	_, _, stored, err := runStore.All()
	if err != nil {
		return RefreshOutcome{}, err
	}
	return RefreshOutcome{
		Dispositions: stored,
		Adopted:      state.reconciliation.BindingUpserts,
		Captured:     timedToDispositions(state.reconciliation.DispositionUpserts),
		Busy:         state.busyReport,
		Conflicts:    sharedConflicts(state.managed, state.busyReport.Shared, state.grid, now),
	}, nil
}

// sharedConflicts reports Managed events that now overlap shared busy time.
// It changes nothing: no moves, no stand-downs. Ordering is by event ID for
// determinism.
func sharedConflicts(managed []calendar.Event, shared grid.Mask, g *grid.Grid, now time.Time) []Conflict {
	if shared.Count() == 0 {
		return nil
	}
	var out []Conflict
	for _, event := range managed {
		if !event.Managed() {
			continue
		}
		start, end, err := event.Span(g.Location())
		if err != nil {
			continue // reconcile already refused corrupt spans into the open
		}
		if start.Before(now) {
			start = now
		}
		if end.After(g.WindowEnd()) {
			end = g.WindowEnd()
		}
		if !start.Before(end) {
			continue
		}
		if g.Overlapping(start, end).And(shared).Count() == 0 {
			continue
		}
		taskID, index, ok := reconcile.TaskRef(event)
		if !ok {
			out = append(out, Conflict{TaskID: "", BlockIndex: -1, EventID: event.ID, Summary: event.Summary})
			continue
		}
		out = append(out, Conflict{TaskID: taskID, BlockIndex: index, EventID: event.ID, Summary: event.Summary})
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.EventID, b.EventID) })
	return out
}
