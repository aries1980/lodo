// Shared read side for schedule/check/refresh: calendar busy time plus
// stored rows reconciled in memory. Never resolves the graph, never writes.
package run

import (
	"context"
	"fmt"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/store"
)

type Calendar interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
	Get(ctx context.Context, id string) (calendar.Event, error)
	Insert(ctx context.Context, e calendar.Event) (calendar.Event, error)
	Patch(ctx context.Context, id string, e calendar.Event) (calendar.Event, error)
	Delete(ctx context.Context, id string) error
}

// CalendarReader is the read-only surface refresh and check need. List and
// Get are write-calendar-only; Busy unions the write plus read calendars.
type CalendarReader interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
	Get(ctx context.Context, id string) (calendar.Event, error)
}

// Store is the system of record surface run needs. *store.Store satisfies it;
// tests substitute a fake to inject a commit failure.
type Store interface {
	All() (bindings []store.Binding, snapshots map[string]model.Snapshot, dispositions []model.Disposition, err error)
	Commit(bindingUpserts []store.Binding, bindingDeletes []store.BindingKey, dispositions []store.TimedDisposition, snapshots map[string]model.Snapshot) error
}

// PendingDeleter is the optional crash-recovery surface: stores that
// persist Lodo-initiated deletes before calendar writes implement it.
// Fakes that do not implement it skip pending tracking; *store.Store does.
type PendingDeleter interface {
	PendingDeletes() ([]store.BindingKey, error)
	SetPendingDeletes([]store.BindingKey) error
}

// RunRecorder is the optional audit surface: stores that persist the
// per-run calendar audit implement it. Fakes that do not implement it skip
// recording; *store.Store does.
type RunRecorder interface {
	RecordRun(command, writeCalendar string, writeEvents int, sources []store.RunSource, recordedAt time.Time) error
}

// pendingDeletes returns Lodo-initiated deletes recorded before calendar
// writes but not yet committed. Empty when the store does not support it.
func pendingDeletes(runStore Store) []store.BindingKey {
	if p, ok := runStore.(PendingDeleter); ok {
		if keys, err := p.PendingDeletes(); err == nil {
			return keys
		}
	}
	return nil
}

// setPendingDeletes records Lodo-initiated deletes before calendar writes.
// No-op when the store does not support it.
func setPendingDeletes(runStore Store, keys []store.BindingKey) error {
	if len(keys) == 0 {
		return nil
	}
	if p, ok := runStore.(PendingDeleter); ok {
		return p.SetPendingDeletes(keys)
	}
	return nil
}

// recordRun persists one schedule/refresh run's calendar audit when the
// store supports it. Forecast never records: it stays store-free by design.
func recordRun(runStore Store, command string, report calendar.BusyReport, now time.Time) error {
	recorder, ok := runStore.(RunRecorder)
	if !ok {
		return nil
	}
	sources := make([]store.RunSource, 0, len(report.Sources))
	for _, source := range report.Sources {
		sources = append(sources, store.RunSource{CalendarID: source.CalendarID, Status: source.Status, Events: source.Events})
	}
	return recorder.RecordRun(command, report.WriteCalendar, report.WriteEvents, sources, now)
}

// calendarState is the shared read side of Schedule/Check/Refresh: parsed
// scheduling settings, the grid, busy time plus Managed events, stored rows
// and the in-memory reconciliation of the two.
type calendarState struct {
	granularity    time.Duration
	location       *time.Location
	grid           *grid.Grid
	busyMask       grid.Mask
	managed        []calendar.Event
	busyReport     calendar.BusyReport
	bindings       []store.Binding
	snapshots      map[string]model.Snapshot
	storedDisps    []model.Disposition
	reconciliation reconcile.Result
}

// loadCalendarState reads busy time and stored rows, then reconciles in
// memory. It never resolves the graph and never writes.
func loadCalendarState(ctx context.Context, cal CalendarReader, runStore Store, cfg config.Config, now time.Time) (calendarState, error) {
	var state calendarState
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return calendarState{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return calendarState{}, err
	}
	g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
	busyMask, managed, busyReport, err := cal.Busy(ctx, g, now)
	if err != nil {
		return calendarState{}, err
	}
	bindings, snapshots, storedDisps, err := runStore.All()
	if err != nil {
		return calendarState{}, err
	}

	// List is bounded to [now, windowEnd), so a binding absent from it is
	// not necessarily a user deletion. Ask the calendar directly about each
	// absent event: a future event that still exists was moved out of the
	// window and must stay pinned, not stand its Block down. An event that
	// has ended is expired: reconcile it as absent so the past Block stands
	// down rather than being recreated. Only a genuinely missing event is a
	// user deletion.
	managed, reconcileBindings, outOfRange, err := collectReconcileBindings(ctx, cal, managed, bindings, location, now)
	if err != nil {
		return calendarState{}, err
	}

	reconciliation, err := reconcile.Reconcile(managed, reconcileBindings, snapshots, granularity, location, now)
	if err != nil {
		return calendarState{}, err
	}
	if err := pinOutOfRange(&reconciliation, outOfRange, bindings, storedDisps, snapshots, granularity, location, now); err != nil {
		return calendarState{}, err
	}
	suppressPendingStoodDown(&reconciliation, pendingDeletes(runStore))
	state = calendarState{
		granularity: granularity, location: location, grid: g,
		busyMask: busyMask, managed: managed, busyReport: busyReport,
		bindings: bindings, snapshots: snapshots, storedDisps: storedDisps, reconciliation: reconciliation,
	}
	return state, nil
}

// collectReconcileBindings resolves bindings absent from List: a future
// event that still exists was moved out of the window and must stay pinned;
// an ended event is expired; only a genuinely missing event is a deletion.
//
// List is bounded to [now, windowEnd), so absence alone proves nothing.
func collectReconcileBindings(ctx context.Context, cal CalendarReader, managed []calendar.Event, bindings []store.Binding, location *time.Location, now time.Time) ([]calendar.Event, []store.Binding, []calendar.Event, error) {
	managedByID := make(map[string]bool, len(managed))
	for _, event := range managed {
		managedByID[event.ID] = true
	}
	reconcileBindings := make([]store.Binding, 0, len(bindings))
	var outOfRange []calendar.Event
	for _, binding := range bindings {
		if managedByID[binding.EventID] {
			reconcileBindings = append(reconcileBindings, binding)
			continue
		}
		event, err := cal.Get(ctx, binding.EventID)
		switch {
		case err == nil:
			if !event.Managed() || event.Cancelled() {
				// The ownership marker was removed, or the user cancelled
				// the event: treat the binding as a deletion, as List-based
				// reconciliation always has.
				reconcileBindings = append(reconcileBindings, binding)
				continue
			}
			_, end, spanErr := event.Span(location)
			if spanErr != nil {
				return nil, nil, nil, spanErr
			}
			if _, _, ok := reconcile.TaskRef(event); !ok {
				// No usable ownership pointer: leave the row untouched
				// rather than reading it as a deletion.
				continue
			}
			if end.After(now) {
				managed = append(managed, event)
				managedByID[event.ID] = true
				outOfRange = append(outOfRange, event)
			}
			reconcileBindings = append(reconcileBindings, binding)
		case isNotFound(err):
			reconcileBindings = append(reconcileBindings, binding)
		default:
			return nil, nil, nil, fmt.Errorf("read managed event %s: %w", binding.EventID, err)
		}
	}
	return managed, reconcileBindings, outOfRange, nil
}

// pinOutOfRange keeps Managed events that sit beyond the List window in the
// plan. Their spans are pinned, so the planner leaves them exactly where the
// user put them and diffPlan matches them instead of patching them back into
// the window. Stored rows that already match the observed span are left
// alone, keeping an unchanged run a no-op. Events without a usable Task
// pointer are never passed here.
func pinOutOfRange(reconciliation *reconcile.Result, observed []calendar.Event, bindings []store.Binding, storedDisps []model.Disposition, snapshots map[string]model.Snapshot, granularity time.Duration, loc *time.Location, now time.Time) error {
	if len(observed) == 0 {
		return nil
	}
	freshPins := make(map[string]bool, len(observed))
	freshBindings := make(map[string]bool, len(observed))
	for _, timed := range reconciliation.DispositionUpserts {
		if timed.Disposition.Kind == model.DispositionPinned {
			freshPins[norm.BlockKey(timed.Disposition.TaskID, timed.Disposition.BlockIndex)] = true
		}
	}
	for _, binding := range reconciliation.BindingUpserts {
		freshBindings[norm.BlockKey(binding.TaskID, binding.BlockIndex)] = true
	}
	storedBindings := make(map[string]store.Binding, len(bindings))
	for _, binding := range bindings {
		storedBindings[norm.BlockKey(binding.TaskID, binding.BlockIndex)] = binding
	}
	storedPins := make(map[string]model.Disposition, len(storedDisps))
	for _, disposition := range storedDisps {
		if disposition.Kind == model.DispositionPinned {
			storedPins[norm.BlockKey(disposition.TaskID, disposition.BlockIndex)] = disposition
		}
	}
	for _, event := range observed {
		taskID, blockIndex, ok := reconcile.TaskRef(event)
		if !ok {
			continue
		}
		start, end, err := event.Span(loc)
		if err != nil {
			return err
		}
		key := norm.BlockKey(taskID, blockIndex)
		slots := norm.Units(end.Sub(start), granularity)
		if !freshBindings[key] {
			stored, known := storedBindings[key]
			if !known || stored.EventID != event.ID || !stored.Start.Equal(start) || !stored.End.Equal(end) || stored.Slots != slots {
				reconciliation.BindingUpserts = append(reconciliation.BindingUpserts, store.Binding{
					TaskID: taskID, BlockIndex: blockIndex, EventID: event.ID,
					Start: start, End: end, Slots: slots,
				})
				freshBindings[key] = true
			}
		}
		if !freshPins[key] {
			stored, known := storedPins[key]
			if !known || !stored.Start.Equal(start) || !stored.End.Equal(end) || stored.Slots != slots {
				reconciliation.DispositionUpserts = append(reconciliation.DispositionUpserts, store.TimedDisposition{
					Disposition: model.Disposition{
						TaskID: taskID, BlockIndex: blockIndex,
						Kind:  model.DispositionPinned,
						Start: start, End: end,
						Slots: slots, Snapshot: snapshots[taskID],
					},
					RecordedAt: now,
				})
				freshPins[key] = true
			}
		}
	}
	return nil
}

// suppressPendingStoodDown drops stood-down dispositions for bindings Lodo
// itself deleted but failed to commit. A successful calendar delete followed
// by a failed store commit leaves the event gone with the binding still
// present; without pending intent the next run would read it as a user
// deletion and silently stand the block down. Pending keys delete the binding
// without a disposition; the plan then decides whether to recreate it.
func suppressPendingStoodDown(reconciliation *reconcile.Result, pending []store.BindingKey) {
	if len(pending) == 0 || len(reconciliation.DispositionUpserts) == 0 {
		return
	}
	pendingSet := make(map[string]bool, len(pending))
	for _, k := range pending {
		pendingSet[norm.BlockKey(k.TaskID, k.BlockIndex)] = true
	}
	kept := reconciliation.DispositionUpserts[:0]
	for _, td := range reconciliation.DispositionUpserts {
		if td.Disposition.Kind == model.DispositionStoodDown &&
			pendingSet[norm.BlockKey(td.Disposition.TaskID, td.Disposition.BlockIndex)] {
			continue
		}
		kept = append(kept, td)
	}
	reconciliation.DispositionUpserts = kept
}
