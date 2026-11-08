// Schedule path: reconcile first, then resolve, plan, write the calendar
// before committing the store (ADR 0007). Now is injected, never read.
package run

import (
	"context"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/store"
)

// WriteOp is one calendar mutation schedule performed, for --verbose and
// --json. Struct-only, never map-keyed.
type WriteOp struct {
	Op         string // insert, patch, delete
	EventID    string
	TaskID     string
	Graph      string // source graph slug; empty in single-graph mode
	BlockIndex int
	Start      time.Time
	End        time.Time
	Summary    string // Task's current text for insert/patch; empty for delete
}

// Orphan is one Managed event with no home in the current plan: its
// task/block does not resolve, or the plan keeps no Block for it. Check
// lists these; schedule deletes them.
type Orphan struct {
	EventID string
	TaskID  string
	Block   int // -1 when the private pointer is unusable
	Summary string
}

// ScheduleOutcome is one schedule run: the resolved graph, the planned
// Schedule, the effective dispositions it planned with, the calendar writes
// it issued, and the orphaned events it removed (or would remove).
type ScheduleOutcome struct {
	Tasks        []model.Task
	Issues       []resolve.Issue
	Schedule     planner.Schedule
	Dispositions []model.Disposition // effective, what the planner saw
	Reconciled   reconcile.Result    // what this run folded in
	Writes       []WriteOp
	Orphans      []Orphan
	// Busy carries the write calendar plus per-read-calendar status for the
	// busy sources line and warnings.
	Busy calendar.BusyReport
}

// resolveAndPlanSources resolves every graph fresh and plans with the
// effective dispositions. Single-graph callers wrap it with one Source.
func resolveAndPlanSources(ctx context.Context, sources []Source, state calendarState, cfg config.Config, now time.Time) (result resolve.Result, combined, effective []model.Disposition, schedule planner.Schedule, err error) {
	result, err = ReadSources(ctx, sources, cfg.Logseq.PrimaryTimeMapsPage(), state.location)
	if err != nil {
		return resolve.Result{}, nil, nil, planner.Schedule{}, err
	}

	// Active dispositions for this run: stored plus newly reconciled,
	// filtered by the freshly resolved snapshots. Pinned always applies;
	// stood-down applies only while inputs still match. Fresh rows win on
	// the same key (a re-moved pin refreshes its span).
	combined = mergeDispositions(state.storedDisps, timedToDispositions(state.reconciliation.DispositionUpserts))
	effective = reconcile.Effective(result.Tasks, combined)

	schedule, err = planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         state.busyMask,
		Dispositions: effective,
		Now:          now,
		Granularity:  state.granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     state.location,
	})
	if err != nil {
		return resolve.Result{}, nil, nil, planner.Schedule{}, err
	}
	return result, combined, effective, schedule, nil
}

// Schedule reconciles first (the anti-clobber rule), resolves, plans with
// active dispositions, diffs the plan against observed Managed events,
// writes the calendar, then commits the store. Calendar writes precede the
// store commit (ADR 0007); a crash between them is adopted on the next run.
func Schedule(ctx context.Context, client logseq.Client, cal Calendar, runStore Store, cfg config.Config, now time.Time) (ScheduleOutcome, error) {
	sources := []Source{{Client: client}}
	return ScheduleSources(ctx, sources, cal, runStore, cfg, now)
}

// ScheduleSources reconciles observed Managed events (in memory), resolves the
// graph(s), plans with effective dispositions, diffs/writes the calendar, then
// commits the store and run audit. Calendar writes happen before store commit
// (ADR 0007). The resolved graph is read each run; snapshots are refreshed only
// by schedule commits.
func ScheduleSources(ctx context.Context, sources []Source, cal Calendar, runStore Store, cfg config.Config, now time.Time) (ScheduleOutcome, error) {
	state, err := loadCalendarState(ctx, cal, runStore, cfg, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	result, _, effective, schedule, err := resolveAndPlanSources(ctx, sources, state, cfg, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	writes, orphans, planBindings := diffPlan(result.Tasks, schedule, state.managed, state.location)

	// Calendar first, store second (ADR 0007).
	if err := applyWrites(ctx, cal, writes); err != nil {
		return ScheduleOutcome{}, err
	}

	// Store commit: reconciled bindings/dispositions plus this plan's
	// bindings, plus refreshed snapshots for every resolved Task. Only
	// schedule writes snapshots.
	snapMap := make(map[string]model.Snapshot, len(result.Tasks))
	for _, task := range result.Tasks {
		snapMap[task.ID] = task.Snapshot()
	}
	allUpserts := append(append([]store.Binding{}, state.reconciliation.BindingUpserts...), planBindings...)
	// Plan bindings win on key collision with reconcile upserts (same span
	// for pinned/adopted when intent is unchanged).
	allUpserts = dedupeBindings(allUpserts)
	bindingDeletes := append(append([]store.BindingKey{}, state.reconciliation.BindingDeletes...), orphansToDeletes(orphans, state.bindings, state.reconciliation)...)
	bindingDeletes = dedupeDeletes(bindingDeletes, planBindings)

	if err := runStore.Commit(allUpserts, bindingDeletes, state.reconciliation.DispositionUpserts, snapMap); err != nil {
		return ScheduleOutcome{}, err
	}
	if err := recordRun(runStore, "schedule", state.busyReport, now); err != nil {
		return ScheduleOutcome{}, err
	}

	return ScheduleOutcome{
		Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		Dispositions: effective, Reconciled: state.reconciliation, Writes: writes, Orphans: orphans,
		Busy: state.busyReport,
	}, nil
}
