package planner

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/set"
)

// topoOrder runs Kahn over Task-to-Task edges (placeable non-Pin only; Pin
// edges are temporal only since Pins are already placed) with the existing
// comparator as tie-break among unrelated Tasks.
func topoOrder(candidates []*taskPlan, lookup planLookup, blockedUnknown map[string][]string) (order []*taskPlan, cycleSet set.Set[string]) {
	indegree := make(map[string]int)
	dependents := make(map[string][]string)
	for _, plan := range candidates {
		if _, blocked := blockedUnknown[plan.task.ID]; blocked {
			continue
		}
		seen := set.New[string]()
		for _, dep := range plan.task.Depends {
			depPlan, ok := lookup.lookup(dep)
			if !ok {
				continue
			}
			if depPlan.task.Pin != nil || depPlan.task.Container {
				continue
			}
			if depPlan.required() == 0 {
				continue // fully stood-down/pinned: temporal only, handled in gating
			}
			if depPlan.task.ID == plan.task.ID {
				continue // self-edge handled as cycle below
			}
			if seen.Add(depPlan.task.ID) {
				indegree[plan.task.ID]++
				dependents[depPlan.task.ID] = append(dependents[depPlan.task.ID], plan.task.ID)
			}
		}
	}
	// Self-dependencies are cycles.
	for _, plan := range candidates {
		for _, dep := range plan.task.Depends {
			if norm.ID(dep) == norm.ID(plan.task.ID) {
				indegree[plan.task.ID]++
			}
		}
	}

	isTimeBound := func(plan *taskPlan) bool { return plan.task.PlanStart != nil || plan.task.PlanEnd != nil }
	lessPlan := func(a, b *taskPlan) bool {
		boundA, boundB := isTimeBound(a), isTimeBound(b)
		if boundA != boundB {
			return boundA && !boundB
		}
		if boundA {
			return timeBoundLess(a.task, b.task)
		}
		return orderBefore(a.task, b.task)
	}
	// origIndegree remembers which tasks had ordering-edge dependencies so
	// explain can report dependency-forced positions truthfully.
	origIndegree := maps.Clone(indegree)
	// orderingDeps lists the ordering-edge dependency IDs for detail strings.
	orderingDeps := func(plan *taskPlan) []string {
		var out []string
		seen := set.New[string]()
		for _, dep := range plan.task.Depends {
			depPlan, ok := lookup.lookup(dep)
			if !ok {
				continue
			}
			if depPlan.task.Pin != nil || depPlan.task.Container {
				continue
			}
			if depPlan.required() == 0 {
				continue
			}
			if depPlan.task.ID == plan.task.ID {
				out = append(out, depPlan.task.ID)
				continue
			}
			if seen.Add(depPlan.task.ID) {
				out = append(out, depPlan.task.ID)
			}
		}
		slices.Sort(out)
		return out
	}
	var avail []*taskPlan
	for _, plan := range candidates {
		if _, blocked := blockedUnknown[plan.task.ID]; blocked {
			continue
		}
		if indegree[plan.task.ID] == 0 {
			avail = append(avail, plan)
		}
	}
	sortAvailablePlans(avail, lessPlan)
	for len(avail) > 0 {
		plan := avail[0]
		peers := append([]*taskPlan(nil), avail...)
		avail = avail[1:]
		order = append(order, plan)
		// Record the actual tie-break that put plan first among ready peers.
		if origIndegree[plan.task.ID] > 0 {
			deps := orderingDeps(plan)
			detail := "dependency-ordered: unblocked after dependencies placed"
			if len(deps) > 0 {
				detail = "dependency-ordered after " + strings.Join(deps, ", ") + "; topological order overrides tie-breaks"
			}
			plan.markTieBreak("dependency", detail)
		} else if len(peers) > 1 {
			key, detail := tieBreakBetween(plan.task, peers[1].task)
			plan.markTieBreak(key, detail)
		} else {
			plan.markTieBreak("only-ready", "only ready task; no peer tie-break")
		}
		for _, depID := range dependents[plan.task.ID] {
			indegree[depID]--
			if indegree[depID] == 0 {
				if depPlan, ok := lookup.byID[depID]; ok {
					if _, blocked := blockedUnknown[depID]; !blocked {
						avail = append(avail, depPlan)
					}
				}
			}
		}
		sortAvailablePlans(avail, lessPlan)
	}
	inOrder := make(set.Set[string], len(order))
	for _, plan := range order {
		inOrder.Add(plan.task.ID)
	}
	// Remaining candidates are either on a cycle or downstream of one.
	// Only the former are cycle members; the latter must report the
	// dependency that blocks them.
	var remaining []*taskPlan
	for _, plan := range candidates {
		if inOrder.Has(plan.task.ID) {
			continue
		}
		if _, blocked := blockedUnknown[plan.task.ID]; blocked {
			continue
		}
		remaining = append(remaining, plan)
	}
	cycleSet = cycleMembers(remaining, lookup)
	for i, plan := range order {
		plan.markPosition(i + 1)
	}
	return order, cycleSet
}

// sortAvailablePlans orders Kahn's frontier deterministically. Extracted so
// the two call sites cannot drift.
func sortAvailablePlans(avail []*taskPlan, less func(a, b *taskPlan) bool) {
	slices.SortFunc(avail, func(a, b *taskPlan) int {
		if less(a, b) {
			return -1
		}
		if less(b, a) {
			return 1
		}
		return 0
	})
}

// cycleMembers returns the Tasks that can reach themselves through
// dependency edges. topoOrder's leftovers also include dependents of a
// cycle; those are blocked by it, not members of it.
func cycleMembers(remaining []*taskPlan, lookup planLookup) set.Set[string] {
	out := set.New[string]()
	for _, plan := range remaining {
		if reachesTask(plan.task.ID, plan.task.ID, lookup, set.New[string]()) {
			out.Add(plan.task.ID)
		}
	}
	return out
}

// reachesTask reports whether following dependency edges from id reaches
// target. Only edges topoOrder counted participate: Pins, containers and
// fully stood-down/pinned dependencies are temporal, not ordering, edges.
func reachesTask(id, target string, lookup planLookup, seen set.Set[string]) bool {
	if !seen.Add(id) {
		return false
	}
	plan, ok := lookup.lookup(id)
	if !ok || plan.task.Container {
		return false
	}
	for _, dep := range plan.task.Depends {
		depPlan, ok := lookup.lookup(dep)
		if !ok || depPlan.task.Pin != nil || depPlan.task.Container || depPlan.required() == 0 {
			continue
		}
		if depPlan.task.ID == target {
			return true
		}
		if reachesTask(depPlan.task.ID, target, lookup, seen) {
			return true
		}
	}
	return false
}

func overdue(t model.Task, today model.CivilDate) bool {
	return t.PlanEnd != nil && t.PlanEnd.Before(today)
}

func timeBoundLess(a, b model.Task) bool {
	switch {
	case a.PlanEnd == nil && b.PlanEnd != nil:
		return false
	case a.PlanEnd != nil && b.PlanEnd == nil:
		return true
	case a.PlanEnd != nil && b.PlanEnd != nil && *a.PlanEnd != *b.PlanEnd:
		return a.PlanEnd.Before(*b.PlanEnd)
	}
	return orderBefore(a, b)
}

func orderBefore(a, b model.Task) bool {
	return compareTasks(a, b) < 0
}

func compareTasks(a, b model.Task) int {
	return cmp.Or(
		cmp.Compare(a.GraphOrder, b.GraphOrder),
		cmp.Compare(a.Priority.Rank(), b.Priority.Rank()),
		cmp.Compare(a.OutlineOrder, b.OutlineOrder),
		cmp.Compare(a.Graph, b.Graph),
		cmp.Compare(a.ID, b.ID),
	)
}

// tieBreakBetween names the first ordering key that distinguishes a (the
// winner) from b (the runner-up), mirroring lessPlan/timeBoundLess/
// compareTasks exactly so explain cannot drift from placement.
func tieBreakBetween(a, b model.Task) (key, detail string) {
	aBound := a.PlanStart != nil || a.PlanEnd != nil
	bBound := b.PlanStart != nil || b.PlanEnd != nil
	if aBound != bBound {
		if aBound {
			return "time-bound-first", "time-bound " + a.ID + " before flexible " + b.ID + "; every time-bound task is considered before any flexible task"
		}
		return "time-bound-first", "flexible " + a.ID + " after time-bound " + b.ID + " (should not win; defensive)"
	}
	if aBound && bBound {
		switch {
		case a.PlanEnd == nil && b.PlanEnd != nil:
			return "plan-end", "plan-end: " + a.ID + " has no plan-end, " + b.ID + " due " + b.PlanEnd.String()
		case a.PlanEnd != nil && b.PlanEnd == nil:
			return "plan-end", "plan-end: " + a.ID + " due " + a.PlanEnd.String() + " before " + b.ID + " with no plan-end"
		case a.PlanEnd != nil && b.PlanEnd != nil && *a.PlanEnd != *b.PlanEnd:
			return "plan-end", "plan-end " + a.PlanEnd.String() + " vs " + b.PlanEnd.String()
		}
	}
	if a.GraphOrder != b.GraphOrder {
		return "graph-order", "graph-order: deterministic cross-graph order"
	}
	if ra, rb := a.Priority.Rank(), b.Priority.Rank(); ra != rb {
		return "priority", "priority " + string(a.Priority) + " (rank " + strconv.Itoa(ra) + ") vs " + string(b.Priority) + " (rank " + strconv.Itoa(rb) + ")"
	}
	if a.OutlineOrder != b.OutlineOrder {
		return "outline-order", "outline-order " + strconv.Itoa(a.OutlineOrder) + " vs " + strconv.Itoa(b.OutlineOrder) + " (depth-first, top-to-bottom Logseq order; moving a block re-prioritises it)"
	}
	if a.Graph != b.Graph {
		return "graph", "graph " + a.Graph + " vs " + b.Graph
	}
	if a.ID != b.ID {
		return "id", "id " + a.ID + " vs " + b.ID
	}
	return "equal", "identical ordering keys"
}
