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
		avail = avail[1:]
		order = append(order, plan)
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
