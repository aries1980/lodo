// The write side of the loop (M5): reconcile first, then plan, then write
// the calendar before committing the store (ADR 0007). Now is injected by
// the caller, never read here; there are no goroutines.
package run

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/store"
)

// Calendar is the full calendar surface schedule needs: read busy time,
// list Managed events, and insert/patch/delete them. List and the writes
// are write-calendar-only; Busy unions the write plus read calendars.
type Calendar interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
	Insert(ctx context.Context, e calendar.Event) (calendar.Event, error)
	Patch(ctx context.Context, id string, e calendar.Event) (calendar.Event, error)
	Delete(ctx context.Context, id string) error
}

// CalendarReader is the read-only surface refresh and check need. List is
// write-calendar-only; Busy unions the write plus read calendars.
type CalendarReader interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
}

// Store is the system of record surface run needs. *store.Store satisfies it;
// tests substitute a fake to inject a commit failure.
type Store interface {
	All() (bindings []store.Binding, snapshots map[string]model.Snapshot, dispositions []model.Disposition, err error)
	Commit(bindingUpserts []store.Binding, bindingDeletes []store.BindingKey, dispositions []store.TimedDisposition, snapshots map[string]model.Snapshot) error
}

// RunRecorder is the optional audit surface: stores that persist the
// per-run calendar audit implement it. Fakes that do not implement it skip
// recording; *store.Store does.
type RunRecorder interface {
	RecordRun(command, writeCalendar string, writeEvents int, sources []store.RunSource, recordedAt time.Time) error
}

// recordRun persists one schedule/refresh run's calendar audit when the
// store supports it. Forecast never records: it stays store-free by design.
func recordRun(st Store, command string, report calendar.BusyReport, now time.Time) error {
	rec, ok := st.(RunRecorder)
	if !ok {
		return nil
	}
	sources := make([]store.RunSource, 0, len(report.Sources))
	for _, s := range report.Sources {
		sources = append(sources, store.RunSource{CalendarID: s.CalendarID, Status: s.Status, Events: s.Events})
	}
	return rec.RecordRun(command, report.WriteCalendar, report.WriteEvents, sources, now)
}

// WriteOp is one calendar mutation schedule performed, for --verbose and
// --json. Struct-only, never map-keyed.
type WriteOp struct {
	Op         string // insert, patch, delete
	EventID    string
	TaskID     string
	BlockIndex int
	Start      time.Time
	End        time.Time
	Summary    string // Task's current text for insert/patch; empty for delete
}

// Orphan is one Managed event with no home in the current plan: its
// task/block does not resolve, or its Block is beyond the window. Check
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

// Conflict is one Managed Block that now overlaps shared busy time: the
// write calendar's event still stands, but a read calendar now claims the
// same hours. Refresh reports these and changes nothing.
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

// calendarState is the shared read side of Schedule/Check/Refresh: parsed
// scheduling settings, the grid, busy time plus Managed events, stored rows
// and the in-memory reconciliation of the two.
type calendarState struct {
	granularity time.Duration
	location    *time.Location
	grid        *grid.Grid
	busyMask    grid.Mask
	managed     []calendar.Event
	busyReport  calendar.BusyReport
	bindings    []store.Binding
	snapshots   map[string]model.Snapshot
	storedDisps []model.Disposition
	rec         reconcile.Result
}

// loadCalendarState reads busy time and stored rows, then reconciles in
// memory. It never resolves the graph and never writes.
func loadCalendarState(ctx context.Context, cal interface {
	Busy(context.Context, *grid.Grid, time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error)
}, st Store, cfg config.Config, now time.Time) (calendarState, error) {
	var cs calendarState
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
	bindings, snapshots, storedDisps, err := st.All()
	if err != nil {
		return calendarState{}, err
	}
	rec, err := reconcile.Reconcile(managed, bindings, snapshots, granularity, location, now)
	if err != nil {
		return calendarState{}, err
	}
	cs = calendarState{
		granularity: granularity, location: location, grid: g,
		busyMask: busyMask, managed: managed, busyReport: busyReport,
		bindings: bindings, snapshots: snapshots, storedDisps: storedDisps, rec: rec,
	}
	return cs, nil
}

// resolveAndPlan resolves the graph fresh and plans with the effective
// dispositions for this run: stored plus newly reconciled, filtered by the
// freshly resolved snapshots.
func resolveAndPlan(ctx context.Context, lc logseq.Client, cs calendarState, cfg config.Config, now time.Time) (result resolve.Result, combined, effective []model.Disposition, schedule planner.Schedule, err error) {
	result, err = Read(ctx, lc, cfg.Logseq.TimeMapsPage, cs.location)
	if err != nil {
		return resolve.Result{}, nil, nil, planner.Schedule{}, err
	}

	// Active dispositions for this run: stored plus newly reconciled,
	// filtered by the freshly resolved snapshots. Pinned always applies;
	// stood-down applies only while inputs still match. Fresh rows win on
	// the same key (a re-moved pin refreshes its span).
	combined = mergeDispositions(cs.storedDisps, timedToDispositions(cs.rec.DispositionUpserts))
	effective = reconcile.Effective(result.Tasks, combined)

	schedule, err = planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         cs.busyMask,
		Dispositions: effective,
		Now:          now,
		Granularity:  cs.granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     cs.location,
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
func Schedule(ctx context.Context, lc logseq.Client, cal Calendar, st Store, cfg config.Config, now time.Time) (ScheduleOutcome, error) {
	cs, err := loadCalendarState(ctx, cal, st, cfg, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	result, _, effective, schedule, err := resolveAndPlan(ctx, lc, cs, cfg, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	writes, orphans, planBindings := diffPlan(result.Tasks, schedule, cs.managed, cs.location)

	// Calendar first, store second (ADR 0007).
	if err := applyWrites(ctx, cal, writes); err != nil {
		return ScheduleOutcome{}, err
	}

	// Store commit: reconciled bindings/dispositions plus this plan's
	// bindings, plus refreshed snapshots for every resolved Task. Only
	// schedule writes snapshots.
	snapMap := make(map[string]model.Snapshot, len(result.Tasks))
	for _, t := range result.Tasks {
		snapMap[t.ID] = t.Snapshot()
	}
	allUpserts := append(append([]store.Binding{}, cs.rec.BindingUpserts...), planBindings...)
	// Plan bindings win on key collision with reconcile upserts (same span
	// for pinned/adopted when intent is unchanged).
	allUpserts = dedupeBindings(allUpserts)
	bindingDeletes := append(append([]store.BindingKey{}, cs.rec.BindingDeletes...), orphansToDeletes(orphans, cs.bindings, cs.rec)...)
	bindingDeletes = dedupeDeletes(bindingDeletes, planBindings)

	if err := st.Commit(allUpserts, bindingDeletes, cs.rec.DispositionUpserts, snapMap); err != nil {
		return ScheduleOutcome{}, err
	}
	if err := recordRun(st, "schedule", cs.busyReport, now); err != nil {
		return ScheduleOutcome{}, err
	}

	return ScheduleOutcome{
		Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		Dispositions: effective, Reconciled: cs.rec, Writes: writes, Orphans: orphans,
		Busy: cs.busyReport,
	}, nil
}

// Refresh pulls the calendar into local state without recomputing: reconcile
// plus store commit only. It never resolves the graph and never writes the
// calendar, so it cannot fabricate intent.
func Refresh(ctx context.Context, cal CalendarReader, st Store, cfg config.Config, now time.Time) (RefreshOutcome, error) {
	cs, err := loadCalendarState(ctx, cal, st, cfg, now)
	if err != nil {
		return RefreshOutcome{}, err
	}
	// Refresh never writes snapshots: it cannot resolve the graph.
	if err := st.Commit(cs.rec.BindingUpserts, cs.rec.BindingDeletes, cs.rec.DispositionUpserts, nil); err != nil {
		return RefreshOutcome{}, err
	}
	if err := recordRun(st, "refresh", cs.busyReport, now); err != nil {
		return RefreshOutcome{}, err
	}
	_, _, stored, err := st.All()
	if err != nil {
		return RefreshOutcome{}, err
	}
	return RefreshOutcome{
		Dispositions: stored,
		Adopted:      cs.rec.BindingUpserts,
		Captured:     timedToDispositions(cs.rec.DispositionUpserts),
		Busy:         cs.busyReport,
		Conflicts:    sharedConflicts(cs.managed, cs.busyReport.Shared, cs.grid, now),
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
	for _, e := range managed {
		if !e.Managed() {
			continue
		}
		start, end, err := e.Span(g.Location())
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
		taskID, index, ok := reconcile.TaskRef(e)
		if !ok {
			out = append(out, Conflict{TaskID: "", BlockIndex: -1, EventID: e.ID, Summary: e.Summary})
			continue
		}
		out = append(out, Conflict{TaskID: taskID, BlockIndex: index, EventID: e.ID, Summary: e.Summary})
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.EventID, b.EventID) })
	return out
}

// diffPlan compares plan Blocks against observed Managed events by
// task+index (event ids are deterministic, so the two coincide for
// Lodo-made events).
//
//   - Plan Block with no observed counterpart → insert with the
//     deterministic id + private ownership + default reminders.
//   - Plan Block with a counterpart at a different span, or with a
//     different title → patch (times plus the Task's current text; Pinned
//     Blocks adopt the text without gaining markers).
//   - Plan Block matching on both instant and title → untouched (the
//     no-op rule: a no-op run issues no writes).
//   - Observed Managed event with no plan Block (stood-down already gone,
//     Task gone, invalid pointer, beyond-window) → delete.
func diffPlan(tasks []model.Task, schedule planner.Schedule, managed []calendar.Event, loc *time.Location) (writes []WriteOp, orphans []Orphan, bindings []store.Binding) {
	textByTask := TextMap(tasks)
	planByKey := make(map[string]model.Block, len(schedule.Blocks))
	for _, b := range schedule.Blocks {
		planByKey[norm.BlockKey(b.TaskID, b.Index)] = b
		bindings = append(bindings, store.Binding{
			TaskID: b.TaskID, BlockIndex: b.Index,
			EventID: reconcile.EventID(b.TaskID, b.Index),
			Start:   b.Start, End: b.End, Slots: b.Slots,
		})
	}
	obsByKey := make(map[string]calendar.Event)
	for _, e := range managed {
		if !e.Managed() {
			continue
		}
		taskID, index, ok := reconcile.TaskRef(e)
		if !ok {
			orphans = append(orphans, Orphan{EventID: e.ID, TaskID: "", Block: -1, Summary: e.Summary})
			writes = append(writes, WriteOp{Op: "delete", EventID: e.ID})
			continue
		}
		obsByKey[norm.BlockKey(taskID, index)] = e
	}
	for key, b := range planByKey {
		e, ok := obsByKey[key]
		if !ok {
			writes = append(writes, WriteOp{
				Op: "insert", EventID: reconcile.EventID(b.TaskID, b.Index),
				TaskID: b.TaskID, BlockIndex: b.Index, Start: b.Start, End: b.End,
				Summary: textByTask[b.TaskID],
			})
			continue
		}
		start, end, err := e.Span(loc)
		if err != nil {
			// A corrupt observed span cannot be compared; patch it back to
			// the plan. The calendar layer already refuses such spans for
			// busy; here honesty means repairing, not skipping.
			writes = append(writes, WriteOp{
				Op: "patch", EventID: e.ID,
				TaskID: b.TaskID, BlockIndex: b.Index, Start: b.Start, End: b.End,
				Summary: textByTask[b.TaskID],
			})
			continue
		}
		wantTitle := textByTask[b.TaskID]
		if start.Equal(b.Start) && end.Equal(b.End) && e.Summary == wantTitle {
			continue // untouched: the no-op rule
		}
		writes = append(writes, WriteOp{
			Op: "patch", EventID: e.ID,
			TaskID: b.TaskID, BlockIndex: b.Index, Start: b.Start, End: b.End,
			Summary: wantTitle,
		})
	}
	for key, e := range obsByKey {
		if _, ok := planByKey[key]; ok {
			continue
		}
		taskID, index, _ := reconcile.TaskRef(e)
		orphans = append(orphans, Orphan{EventID: e.ID, TaskID: taskID, Block: index, Summary: e.Summary})
		writes = append(writes, WriteOp{Op: "delete", EventID: e.ID})
	}
	slices.SortFunc(writes, func(a, b WriteOp) int {
		if a.Op != b.Op {
			return strings.Compare(a.Op, b.Op)
		}
		return strings.Compare(a.EventID, b.EventID)
	})
	slices.SortFunc(orphans, func(a, b Orphan) int { return strings.Compare(a.EventID, b.EventID) })
	slices.SortFunc(bindings, func(a, b store.Binding) int {
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return a.BlockIndex - b.BlockIndex
	})
	return writes, orphans, bindings
}

// applyWrites issues the calendar mutations in a stable order: deletes,
// patches, then inserts (all ordered by event id by diffPlan). A delete
// 404 is already-gone success; anything else is operational.
func applyWrites(ctx context.Context, cal Calendar, writes []WriteOp) error {
	for _, w := range writes {
		switch w.Op {
		case "delete":
			if err := cal.Delete(ctx, w.EventID); err != nil {
				if isNotFound(err) {
					continue
				}
				return err
			}
		case "patch":
			e := patchPayload(w)
			if _, err := cal.Patch(ctx, w.EventID, e); err != nil {
				return err
			}
		case "insert":
			e := insertPayload(w)
			if _, err := cal.Insert(ctx, e); err != nil {
				return err
			}
		default:
			return fmt.Errorf("run: unknown write op %q", w.Op)
		}
	}
	return nil
}

func insertPayload(w WriteOp) calendar.Event {
	e := eventTimes(w)
	e.ID = w.EventID
	e.ExtendedProperties = calendar.Extended{Private: map[string]string{
		"lodo": "true", "task": w.TaskID, "block": strconv.Itoa(w.BlockIndex),
	}}
	e.Reminders = &calendar.Reminders{UseDefault: true}
	return e
}

func patchPayload(w WriteOp) calendar.Event {
	return eventTimes(w)
}

func eventTimes(w WriteOp) calendar.Event {
	tz := ""
	if !w.Start.IsZero() {
		if loc := w.Start.Location(); loc != nil {
			tz = loc.String()
		}
	}
	return calendar.Event{
		Summary: w.Summary,
		Start:   calendar.EventTime{DateTime: w.Start.Format(time.RFC3339), TimeZone: tz},
		End:     calendar.EventTime{DateTime: w.End.Format(time.RFC3339), TimeZone: tz},
	}
}

// CheckOutcome is one check run: the graph report plus the state section.
// No store commit and no calendar writes happen here.
type CheckOutcome struct {
	Tasks           []model.Task
	Issues          []resolve.Issue
	Schedule        planner.Schedule
	Dispositions    []model.Disposition // effective, what the planner saw
	AllDispositions []model.Disposition // stored plus in-memory reconciled
	Orphans         []Orphan
	StoodDown       []model.Disposition // effective stood-down, for display
	// Busy carries the write calendar plus per-read-calendar status and the
	// lodo-on-read warnings.
	Busy calendar.BusyReport
}

// Check reads the graph, the calendar's busy time and the local store, and
// plans with active dispositions — all read-only. It reconciles in memory
// (without committing) so fresh moves shape the plan it judges, but nothing
// is written anywhere.
func Check(ctx context.Context, lc logseq.Client, cal CalendarReader, st Store, cfg config.Config, now time.Time) (CheckOutcome, error) {
	cs, err := loadCalendarState(ctx, cal, st, cfg, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	result, combined, effective, schedule, err := resolveAndPlan(ctx, lc, cs, cfg, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	_, orphans, _ := diffPlan(result.Tasks, schedule, cs.managed, cs.location)
	var stood []model.Disposition
	for _, d := range effective {
		if d.Kind == model.DispositionStoodDown {
			stood = append(stood, d)
		}
	}
	return CheckOutcome{
		Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		Dispositions: effective, AllDispositions: combined,
		Orphans: orphans, StoodDown: stood, Busy: cs.busyReport,
	}, nil
}

func timedToDispositions(in []store.TimedDisposition) []model.Disposition {
	out := make([]model.Disposition, 0, len(in))
	for _, td := range in {
		out = append(out, td.Disposition)
	}
	return out
}

// mergeDispositions overlays fresh rows onto stored ones by task+index.
// Fresh wins: a re-moved pin refreshes its span rather than duplicating it
// (which the planner would reject as two dispositions for one Block).
func mergeDispositions(stored, fresh []model.Disposition) []model.Disposition {
	byKey := make(map[string]model.Disposition, len(stored)+len(fresh))
	order := []string{}
	put := func(d model.Disposition) {
		key := norm.BlockKey(d.TaskID, d.BlockIndex)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = d
	}
	for _, d := range stored {
		put(d)
	}
	for _, d := range fresh {
		// Fresh winning keeps first-seen order, overwriting the value. The
		// planner sorts by key only through its map, so order here never
		// leaks into output.
		put(d)
	}
	out := make([]model.Disposition, 0, len(byKey))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	slices.SortFunc(out, func(a, b model.Disposition) int {
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return a.BlockIndex - b.BlockIndex
	})
	return out
}

func dedupeBindings(in []store.Binding) []store.Binding {
	byKey := make(map[string]store.Binding, len(in))
	for _, b := range in {
		byKey[norm.BlockKey(b.TaskID, b.BlockIndex)] = b // later (plan) wins
	}
	out := make([]store.Binding, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b store.Binding) int {
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return a.BlockIndex - b.BlockIndex
	})
	return out
}

func dedupeDeletes(in []store.BindingKey, keep []store.Binding) []store.BindingKey {
	keepSet := make(map[string]bool, len(keep))
	for _, b := range keep {
		keepSet[norm.BlockKey(b.TaskID, b.BlockIndex)] = true
	}
	seen := map[string]bool{}
	var out []store.BindingKey
	for _, k := range in {
		key := norm.BlockKey(k.TaskID, k.BlockIndex)
		if keepSet[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, k)
	}
	return out
}

// orphansToDeletes turns observed-not-in-plan events with usable pointers
// into binding deletes, so a Task gone from the graph does not leave stale
// rows. Events with unusable pointers have no row to delete by key; the
// reconcile adoption never created one.
func orphansToDeletes(orphans []Orphan, bindings []store.Binding, rec reconcile.Result) []store.BindingKey {
	byEvent := make(map[string]store.Binding, len(bindings))
	for _, b := range bindings {
		byEvent[b.EventID] = b
	}
	for _, u := range rec.BindingUpserts {
		byEvent[u.EventID] = u
	}
	var out []store.BindingKey
	for _, o := range orphans {
		if o.Block < 0 {
			continue
		}
		if b, ok := byEvent[o.EventID]; ok {
			out = append(out, store.BindingKey{TaskID: b.TaskID, BlockIndex: b.BlockIndex})
			continue
		}
		// Adopted-then-orphaned in the same run, or a deterministic id the
		// store never saw: delete by the pointer itself.
		out = append(out, store.BindingKey{TaskID: o.TaskID, BlockIndex: o.Block})
	}
	return out
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "404") || strings.Contains(s, "notFound") || strings.Contains(s, "no such event")
}
