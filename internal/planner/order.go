package planner

import (
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// topoOrder runs Kahn over Task-to-Task edges (placeable non-Pin only; Pin
// edges are temporal only since Pins are already placed) with the existing
// comparator as tie-break among unrelated Tasks.
func topoOrder(candidates []*taskPlan, lookup planLookup, blockedUnknown map[string][]string) (order []*taskPlan, cycleSet map[string]bool) {
	indegree := make(map[string]int)
	dependents := make(map[string][]string)
	for _, plan := range candidates {
		if _, blocked := blockedUnknown[plan.task.ID]; blocked {
			continue
		}
		seen := map[string]bool{}
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
			if !seen[depPlan.task.ID] {
				seen[depPlan.task.ID] = true
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
	origIndegree := make(map[string]int, len(indegree))
	for id, n := range indegree {
		origIndegree[id] = n
	}
	// orderingDeps lists the ordering-edge dependency IDs for detail strings.
	orderingDeps := func(plan *taskPlan) []string {
		var out []string
		seen := map[string]bool{}
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
			if !seen[depPlan.task.ID] {
				seen[depPlan.task.ID] = true
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
			plan.decision.Ordering.TieBreak = "dependency"
			if len(deps) == 0 {
				plan.decision.Ordering.TieBreakDetail = "dependency-ordered: unblocked after dependencies placed"
			} else {
				plan.decision.Ordering.TieBreakDetail = "dependency-ordered after " + strings.Join(deps, ", ") + "; topological order overrides tie-breaks"
			}
		} else if len(peers) > 1 {
			key, detail := tieBreakBetween(plan.task, peers[1].task)
			plan.decision.Ordering.TieBreak = key
			plan.decision.Ordering.TieBreakDetail = detail
		} else {
			plan.decision.Ordering.TieBreak = "only-ready"
			plan.decision.Ordering.TieBreakDetail = "only ready task; no peer tie-break"
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
	inOrder := map[string]bool{}
	for _, plan := range order {
		inOrder[plan.task.ID] = true
	}
	// Remaining candidates are either on a cycle or downstream of one.
	// Only the former are cycle members; the latter must report the
	// dependency that blocks them.
	var remaining []*taskPlan
	for _, plan := range candidates {
		if inOrder[plan.task.ID] {
			continue
		}
		if _, blocked := blockedUnknown[plan.task.ID]; blocked {
			continue
		}
		remaining = append(remaining, plan)
	}
	cycleSet = cycleMembers(remaining, lookup)
	for i, plan := range order {
		plan.decision.Ordering.Position = i + 1
		plan.decision.Ordering.Total = len(order)
	}
	for _, plan := range candidates {
		if plan.decision.Ordering.Total == 0 {
			plan.decision.Ordering.Total = len(order)
		}
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
func cycleMembers(remaining []*taskPlan, lookup planLookup) map[string]bool {
	out := map[string]bool{}
	for _, plan := range remaining {
		if reachesTask(plan.task.ID, plan.task.ID, lookup, map[string]bool{}) {
			out[plan.task.ID] = true
		}
	}
	return out
}

// reachesTask reports whether following dependency edges from id reaches
// target. Only edges topoOrder counted participate: Pins, containers and
// fully stood-down/pinned dependencies are temporal, not ordering, edges.
func reachesTask(id, target string, lookup planLookup, seen map[string]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
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
	if a.GraphOrder != b.GraphOrder {
		return a.GraphOrder - b.GraphOrder
	}
	if ra, rb := a.Priority.Rank(), b.Priority.Rank(); ra != rb {
		return ra - rb
	}
	if a.OutlineOrder != b.OutlineOrder {
		return a.OutlineOrder - b.OutlineOrder
	}
	if a.Graph != b.Graph {
		return strings.Compare(a.Graph, b.Graph)
	}
	return strings.Compare(a.ID, b.ID)
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
		return "priority", "priority " + string(a.Priority) + " (rank " + itoa(ra) + ") vs " + string(b.Priority) + " (rank " + itoa(rb) + ")"
	}
	if a.OutlineOrder != b.OutlineOrder {
		return "outline-order", "outline-order " + itoa(a.OutlineOrder) + " vs " + itoa(b.OutlineOrder) + " (depth-first, top-to-bottom Logseq order; moving a block re-prioritises it)"
	}
	if a.Graph != b.Graph {
		return "graph", "graph " + a.Graph + " vs " + b.Graph
	}
	if a.ID != b.ID {
		return "id", "id " + a.ID + " vs " + b.ID
	}
	return "equal", "identical ordering keys"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
