// Diff helpers: compare the planned Blocks against observed Managed events
// (diffPlan) plus pure list merges used by schedule/refresh. No I/O.
package run

import (
	"cmp"
	"slices"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/set"
	"github.com/aries1980/lodo/internal/store"
)

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
//     Task gone, invalid pointer) → delete.
func diffPlan(tasks []model.Task, schedule planner.Schedule, managed []calendar.Event, loc *time.Location) (writes []WriteOp, orphans []Orphan, bindings []store.Binding) {
	textByTask := model.TextByTaskID(tasks)
	graphByTask := model.GraphByTaskID(tasks)
	planByKey := make(map[string]model.Block, len(schedule.Blocks))
	for _, planBlock := range schedule.Blocks {
		planByKey[norm.BlockKey(planBlock.TaskID, planBlock.Index)] = planBlock
		bindings = append(bindings, store.Binding{
			TaskID: planBlock.TaskID, BlockIndex: planBlock.Index,
			EventID: reconcile.EventID(planBlock.TaskID, planBlock.Index),
			Start:   planBlock.Start, End: planBlock.End, Slots: planBlock.Slots,
		})
	}
	obsByKey := make(map[string]calendar.Event)
	for _, observed := range managed {
		if !observed.Managed() {
			continue
		}
		taskID, index, ok := reconcile.TaskRef(observed)
		if !ok {
			orphans = append(orphans, Orphan{EventID: observed.ID, TaskID: "", Block: -1, Summary: observed.Summary})
			writes = append(writes, WriteOp{Op: "delete", EventID: observed.ID})
			continue
		}
		obsByKey[norm.BlockKey(taskID, index)] = observed
	}
	for key, planBlock := range planByKey {
		observed, ok := obsByKey[key]
		if !ok {
			writes = append(writes, WriteOp{
				Op: "insert", EventID: reconcile.EventID(planBlock.TaskID, planBlock.Index),
				TaskID: planBlock.TaskID, Graph: graphByTask[planBlock.TaskID], BlockIndex: planBlock.Index, Start: planBlock.Start, End: planBlock.End,
				Summary: textByTask[planBlock.TaskID],
			})
			continue
		}
		start, end, err := observed.Span(loc)
		if err != nil {
			// A corrupt observed span cannot be compared; patch it back to
			// the plan. The calendar layer already refuses such spans for
			// busy; here honesty means repairing, not skipping.
			writes = append(writes, WriteOp{
				Op: "patch", EventID: observed.ID,
				TaskID: planBlock.TaskID, Graph: graphByTask[planBlock.TaskID], BlockIndex: planBlock.Index, Start: planBlock.Start, End: planBlock.End,
				Summary: textByTask[planBlock.TaskID],
			})
			continue
		}
		wantTitle := textByTask[planBlock.TaskID]
		if start.Equal(planBlock.Start) && end.Equal(planBlock.End) && observed.Summary == wantTitle {
			continue // untouched: the no-op rule
		}
		writes = append(writes, WriteOp{
			Op: "patch", EventID: observed.ID,
			TaskID: planBlock.TaskID, Graph: graphByTask[planBlock.TaskID], BlockIndex: planBlock.Index, Start: planBlock.Start, End: planBlock.End,
			Summary: wantTitle,
		})
	}
	for key, observed := range obsByKey {
		if _, ok := planByKey[key]; ok {
			continue
		}
		taskID, index, _ := reconcile.TaskRef(observed)
		orphans = append(orphans, Orphan{EventID: observed.ID, TaskID: taskID, Block: index, Summary: observed.Summary})
		writes = append(writes, WriteOp{Op: "delete", EventID: observed.ID})
	}
	slices.SortFunc(writes, func(a, b WriteOp) int {
		if a.Op != b.Op {
			return cmp.Compare(writeOrder(a.Op), writeOrder(b.Op))
		}
		return cmp.Compare(a.EventID, b.EventID)
	})
	slices.SortFunc(orphans, func(a, b Orphan) int { return cmp.Compare(a.EventID, b.EventID) })
	slices.SortFunc(bindings, func(a, b store.Binding) int {
		return a.Compare(b.BindingKey)
	})
	return writes, orphans, bindings
}

func writeOrder(op string) int {
	switch op {
	case "delete":
		return 0
	case "patch":
		return 1
	default:
		return 2
	}
}

func timedToDispositions(in []store.TimedDisposition) []model.Disposition {
	out := make([]model.Disposition, 0, len(in))
	for _, timed := range in {
		out = append(out, timed.Disposition)
	}
	return out
}

// mergeDispositions overlays fresh rows onto stored ones by task+index.
// Fresh wins: a re-moved pin refreshes its span rather than duplicating it
// (which the planner would reject as two dispositions for one Block).
func mergeDispositions(stored, fresh []model.Disposition) []model.Disposition {
	byKey := make(map[string]model.Disposition, len(stored)+len(fresh))
	order := []string{}
	put := func(disposition model.Disposition) {
		key := norm.BlockKey(disposition.TaskID, disposition.BlockIndex)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = disposition
	}
	for _, disposition := range stored {
		put(disposition)
	}
	for _, disposition := range fresh {
		// Fresh winning keeps first-seen order, overwriting the value. The
		// planner sorts by key only through its map, so order here never
		// leaks into output.
		put(disposition)
	}
	out := make([]model.Disposition, 0, len(byKey))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	slices.SortFunc(out, func(a, b model.Disposition) int {
		return cmp.Or(cmp.Compare(a.TaskID, b.TaskID), cmp.Compare(a.BlockIndex, b.BlockIndex))
	})
	return out
}

func dedupeBindings(in []store.Binding) []store.Binding {
	byKey := make(map[store.BindingKey]store.Binding, len(in))
	for _, binding := range in {
		byKey[binding.BindingKey] = binding // later (plan) wins
	}
	out := make([]store.Binding, 0, len(byKey))
	for _, binding := range byKey {
		out = append(out, binding)
	}
	slices.SortFunc(out, func(a, b store.Binding) int {
		return a.Compare(b.BindingKey)
	})
	return out
}

func dedupeDeletes(in []store.BindingKey, keep []store.Binding) []store.BindingKey {
	keepSet := make(set.Set[store.BindingKey], len(keep))
	for _, binding := range keep {
		keepSet.Add(binding.BindingKey)
	}
	seen := set.New[store.BindingKey]()
	var out []store.BindingKey
	for _, delKey := range in {
		if keepSet.Has(delKey) || !seen.Add(delKey) {
			continue
		}
		out = append(out, delKey)
	}
	return out
}

// orphansToDeletes turns observed-not-in-plan events with usable pointers
// into binding deletes, so a Task gone from the graph does not leave stale
// rows. Events with unusable pointers have no row to delete by key; the
// reconcile adoption never created one.
func orphansToDeletes(orphans []Orphan, bindings []store.Binding, reconciliation reconcile.Result) []store.BindingKey {
	byEvent := make(map[string]store.Binding, len(bindings))
	for _, binding := range bindings {
		byEvent[binding.EventID] = binding
	}
	for _, upsert := range reconciliation.BindingUpserts {
		byEvent[upsert.EventID] = upsert
	}
	var out []store.BindingKey
	for _, orphan := range orphans {
		if orphan.Block < 0 {
			continue
		}
		if binding, ok := byEvent[orphan.EventID]; ok {
			out = append(out, binding.BindingKey)
			continue
		}
		// Adopted-then-orphaned in the same run, or a deterministic id the
		// store never saw: delete by the pointer itself.
		out = append(out, store.BindingKey{TaskID: orphan.TaskID, BlockIndex: orphan.Block})
	}
	return out
}
