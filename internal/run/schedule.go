// The write side of the loop (M5): reconcile first, then plan, then write
// the calendar before committing the store (ADR 0007). Now is injected by
// the caller, never read here; there are no goroutines.
package run

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/store"
)

// Calendar is the full calendar surface schedule needs: read busy time,
// list Managed events, and insert/patch/delete them.
type Calendar interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
	Insert(ctx context.Context, e calendar.Event) (calendar.Event, error)
	Patch(ctx context.Context, id string, e calendar.Event) (calendar.Event, error)
	Delete(ctx context.Context, id string) error
}

// CalendarReader is the read-only surface refresh and check need.
type CalendarReader interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []calendar.Event, error)
	List(ctx context.Context, from, to time.Time) ([]calendar.Event, error)
}

// Store is the system of record surface run needs. *store.Store satisfies it;
// tests substitute a fake to inject a commit failure.
type Store interface {
	All() (bindings []store.Binding, snapshots map[string]model.Snapshot, dispositions []model.Disposition, err error)
	Commit(bindingUpserts []store.Binding, bindingDeletes []store.BindingKey, dispositions []store.TimedDisposition, snapshots map[string]model.Snapshot) error
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
}

// RefreshOutcome is one refresh run: pull-only, never recomputing.
type RefreshOutcome struct {
	Dispositions []model.Disposition // all stored after commit, ordered
	Adopted      []store.Binding     // binding rows adopted/updated this run
	Captured     []model.Disposition // disposition rows recorded this run
}

// Schedule reconciles first (the anti-clobber rule), resolves, plans with
// active dispositions, diffs the plan against observed Managed events,
// writes the calendar, then commits the store. Calendar writes precede the
// store commit (ADR 0007); a crash between them is adopted on the next run.
func Schedule(ctx context.Context, lc logseq.Client, cal Calendar, st Store, cfg config.Config, now time.Time) (ScheduleOutcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return ScheduleOutcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return ScheduleOutcome{}, err
	}
	g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)

	busyMask, managed, err := cal.Busy(ctx, g, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}
	bindings, snapshots, storedDisps, err := st.All()
	if err != nil {
		return ScheduleOutcome{}, err
	}
	rec, err := reconcile.Reconcile(managed, bindings, snapshots, granularity, location, now)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	result, err := Read(ctx, lc, cfg.Logseq.TimeMapsPage, location)
	if err != nil {
		return ScheduleOutcome{}, err
	}

	// Active dispositions for this run: stored plus newly reconciled,
	// filtered by the freshly resolved snapshots. Pinned always applies;
	// stood-down applies only while inputs still match. Fresh rows win on
	// the same key (a re-moved pin refreshes its span).
	combined := mergeDispositions(storedDisps, timedToDispositions(rec.DispositionUpserts))

	effective := reconcile.Effective(result.Tasks, combined)

	schedule, err := planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         busyMask,
		Dispositions: effective,
		Now:          now,
		Granularity:  granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     location,
	})
	if err != nil {
		return ScheduleOutcome{}, err
	}

	writes, orphans, planBindings := diffPlan(result.Tasks, schedule, managed, location)

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
	allUpserts := append(append([]store.Binding{}, rec.BindingUpserts...), planBindings...)
	// Plan bindings win on key collision with reconcile upserts (same span
	// for pinned/adopted when intent is unchanged).
	allUpserts = dedupeBindings(allUpserts)
	bindingDeletes := append(append([]store.BindingKey{}, rec.BindingDeletes...), orphansToDeletes(orphans, bindings, rec)...)
	bindingDeletes = dedupeDeletes(bindingDeletes, planBindings)

	if err := st.Commit(allUpserts, bindingDeletes, rec.DispositionUpserts, snapMap); err != nil {
		return ScheduleOutcome{}, err
	}

	return ScheduleOutcome{
		Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		Dispositions: effective, Reconciled: rec, Writes: writes, Orphans: orphans,
	}, nil
}

// Refresh pulls the calendar into local state without recomputing: reconcile
// plus store commit only. It never resolves the graph and never writes the
// calendar, so it cannot fabricate intent.
func Refresh(ctx context.Context, cal CalendarReader, st Store, cfg config.Config, now time.Time) (RefreshOutcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return RefreshOutcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return RefreshOutcome{}, err
	}
	g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
	_, managed, err := cal.Busy(ctx, g, now)
	if err != nil {
		return RefreshOutcome{}, err
	}
	bindings, snapshots, _, err := st.All()
	if err != nil {
		return RefreshOutcome{}, err
	}
	rec, err := reconcile.Reconcile(managed, bindings, snapshots, granularity, location, now)
	if err != nil {
		return RefreshOutcome{}, err
	}
	// Refresh never writes snapshots: it cannot resolve the graph.
	if err := st.Commit(rec.BindingUpserts, rec.BindingDeletes, rec.DispositionUpserts, nil); err != nil {
		return RefreshOutcome{}, err
	}
	_, _, stored, err := st.All()
	if err != nil {
		return RefreshOutcome{}, err
	}
	return RefreshOutcome{
		Dispositions: stored,
		Adopted:      rec.BindingUpserts,
		Captured:     timedToDispositions(rec.DispositionUpserts),
	}, nil
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
	textByTask := make(map[string]string, len(tasks))
	for _, t := range tasks {
		textByTask[t.ID] = t.Text
	}
	planByKey := make(map[string]model.Block, len(schedule.Blocks))
	for _, b := range schedule.Blocks {
		planByKey[planKey(b.TaskID, b.Index)] = b
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
		obsByKey[planKey(taskID, index)] = e
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
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].Op != writes[j].Op {
			return writes[i].Op < writes[j].Op
		}
		return writes[i].EventID < writes[j].EventID
	})
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].EventID < orphans[j].EventID })
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].TaskID != bindings[j].TaskID {
			return bindings[i].TaskID < bindings[j].TaskID
		}
		return bindings[i].BlockIndex < bindings[j].BlockIndex
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
	tz := ""
	if !w.Start.IsZero() {
		if loc := w.Start.Location(); loc != nil {
			tz = loc.String()
		}
	}
	return calendar.Event{
		ID:      w.EventID,
		Summary: w.Summary,
		Start:   calendar.EventTime{DateTime: w.Start.Format(time.RFC3339), TimeZone: tz},
		End:     calendar.EventTime{DateTime: w.End.Format(time.RFC3339), TimeZone: tz},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": w.TaskID, "block": strconv.Itoa(w.BlockIndex),
		}},
		Reminders: &calendar.Reminders{UseDefault: true},
	}
}

func patchPayload(w WriteOp) calendar.Event {
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

func planKey(taskID string, index int) string { return taskID + "\x00" + strconv.Itoa(index) }

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
}

// Check reads the graph, the calendar's busy time and the local store, and
// plans with active dispositions — all read-only. It reconciles in memory
// (without committing) so fresh moves shape the plan it judges, but nothing
// is written anywhere.
func Check(ctx context.Context, lc logseq.Client, cal CalendarReader, st Store, cfg config.Config, now time.Time) (CheckOutcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return CheckOutcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return CheckOutcome{}, err
	}
	g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
	busyMask, managed, err := cal.Busy(ctx, g, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	bindings, snapshots, storedDisps, err := st.All()
	if err != nil {
		return CheckOutcome{}, err
	}
	rec, err := reconcile.Reconcile(managed, bindings, snapshots, granularity, location, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	result, err := Read(ctx, lc, cfg.Logseq.TimeMapsPage, location)
	if err != nil {
		return CheckOutcome{}, err
	}
	combined := mergeDispositions(storedDisps, timedToDispositions(rec.DispositionUpserts))
	effective := reconcile.Effective(result.Tasks, combined)
	schedule, err := planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         busyMask,
		Dispositions: effective,
		Now:          now,
		Granularity:  granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     location,
	})
	if err != nil {
		return CheckOutcome{}, err
	}
	_, orphans, _ := diffPlan(result.Tasks, schedule, managed, location)
	var stood []model.Disposition
	for _, d := range effective {
		if d.Kind == model.DispositionStoodDown {
			stood = append(stood, d)
		}
	}
	return CheckOutcome{
		Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		Dispositions: effective, AllDispositions: combined,
		Orphans: orphans, StoodDown: stood,
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
		key := planKey(d.TaskID, d.BlockIndex)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = d
	}
	for _, d := range stored {
		put(d)
	}
	for _, d := range fresh {
		// Fresh wining must also move to the end for determinism? No: keep
		// first-seen order, overwrite the value. The planner sorts by key
		// only through its map, so order here never leaks into output.
		key := planKey(d.TaskID, d.BlockIndex)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = d
	}
	out := make([]model.Disposition, 0, len(byKey))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].BlockIndex < out[j].BlockIndex
	})
	return out
}

func dedupeBindings(in []store.Binding) []store.Binding {
	byKey := make(map[string]store.Binding, len(in))
	for _, b := range in {
		byKey[planKey(b.TaskID, b.BlockIndex)] = b // later (plan) wins
	}
	out := make([]store.Binding, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].BlockIndex < out[j].BlockIndex
	})
	return out
}

func dedupeDeletes(in []store.BindingKey, keep []store.Binding) []store.BindingKey {
	keepSet := make(map[string]bool, len(keep))
	for _, b := range keep {
		keepSet[planKey(b.TaskID, b.BlockIndex)] = true
	}
	seen := map[string]bool{}
	var out []store.BindingKey
	for _, k := range in {
		key := planKey(k.TaskID, k.BlockIndex)
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
	return contains(s, "404") || contains(s, "notFound") || contains(s, "no such event")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
