package planner

import (
	"slices"
	"time"
)

// reportInput bundles buildReport's inputs so the signature stays readable.
// All maps/slices are read-only except scope.ends, which pin-violation
// checks read.
type reportInput struct {
	candidates     []*taskPlan
	order          []*taskPlan
	pins           []*taskPlan
	lookup         planLookup
	cycleSet       map[string]bool
	blockedUnknown map[string][]string
	blockedReason  map[string]Reason
	blockedBy      map[string][]string
	scope          *placementScope
}

// placeOrdered gates each Task on its dependencies (Q8, Q14) and places the
// survivors. It records ends as the max of pinned + new flexible blocks.
func placeOrdered(planBuilder *builder, order []*taskPlan, lookup planLookup, cycleSet map[string]bool, scope *placementScope) (blockedReason map[string]Reason, blockedBy map[string][]string) {
	blockedReason = map[string]Reason{}
	blockedBy = map[string][]string{}

	for _, plan := range order {
		if cycleSet[plan.task.ID] {
			plan.decision.Dependency.CycleMember = true
		}
		if plan.task.PlanStart != nil || plan.task.PlanEnd != nil {
			plan.decision.Ordering.Kind = "time-bound"
		} else {
			plan.decision.Ordering.Kind = "flexible"
		}

		if markBeyondHorizon(plan, scope) {
			continue // not due yet; left unplaced silently
		}
		// Full-gate: any dep stood-down, shortfall, unplaced, or cycle
		// blocks the plan entirely (Q8, Q14).
		blockingIDs, depEnd, hasDepEnd, blocked := dependencyGate(plan, lookup, cycleSet, scope.ends)
		plan.decision.Eligibility.Considered = true
		if hasDepEnd {
			plan.decision.Dependency.LowerBound = depEnd
		}
		if blocked {
			plan.shortfall = plan.required()
			slices.Sort(blockingIDs)
			blockedReason[plan.task.ID] = ReasonBlockedByDependency
			blockedBy[plan.task.ID] = blockingIDs
			plan.decision.Dependency.Blocked = true
			plan.decision.Dependency.BlockReason = string(ReasonBlockedByDependency)
			plan.decision.Dependency.BlockedBy = append([]string(nil), blockingIDs...)
			continue
		}
		startBound := scope.now
		if hasDepEnd && depEnd.After(startBound) {
			startBound = depEnd
		}
		planBuilder.placeWithBound(plan, scope.maps, overdue(plan.task, scope.today), startBound)
		recordPlacementEnd(plan, planBuilder, scope.ends)
	}
	return blockedReason, blockedBy
}

// markBeyondHorizon flags plans not due yet and reports whether to skip.
func markBeyondHorizon(plan *taskPlan, scope *placementScope) bool {
	beyondHorizon := plan.task.PlanStart != nil && !plan.task.PlanStart.Before(scope.horizonEnd)
	if !beyondHorizon {
		return false
	}
	plan.decision.Eligibility.Considered = false
	plan.decision.Eligibility.BeyondHorizon = true
	plan.decision.Eligibility.SkipReason = "beyond scheduling horizon: not due yet"
	plan.decision.Window.Overdue = false
	if scope.horizonEnd.Year != 0 {
		plan.decision.Window.HorizonEnd = scope.horizonEnd.String()
	}
	return true
}

// dependencyGate checks finish-to-start predecessors: explicit depends plus
// sequential-chain edges already expanded into Depends.
func dependencyGate(plan *taskPlan, lookup planLookup, cycleSet map[string]bool, ends map[string]time.Time) (blockingIDs []string, depEnd time.Time, hasDepEnd bool, blocked bool) {
	for _, dep := range plan.task.Depends {
		depPlan, ok := lookup.lookup(dep)
		if !ok {
			blockingIDs = append(blockingIDs, dep)
			blocked = true
			continue
		}
		if depPlan.task.Container {
			continue
		}
		if depPlan.task.Pin != nil {
			if end, ok := ends[depPlan.task.ID]; ok {
				if !hasDepEnd || end.After(depEnd) {
					depEnd = end
					hasDepEnd = true
				}
			}
			continue
		}
		if cycleSet[depPlan.task.ID] {
			blockingIDs = append(blockingIDs, depPlan.task.ID)
			blocked = true
			continue
		}
		if depPlan.stoodDown > 0 {
			blockingIDs = append(blockingIDs, depPlan.task.ID)
			blocked = true
			continue
		}
		if depPlan.shortfall > 0 {
			blockingIDs = append(blockingIDs, depPlan.task.ID)
			blocked = true
			continue
		}
		end, ok := ends[depPlan.task.ID]
		if !ok {
			// Dep beyond horizon (silently skipped) or otherwise
			// unplaced: the plan waits.
			blockingIDs = append(blockingIDs, depPlan.task.ID)
			blocked = true
			continue
		}
		if !hasDepEnd || end.After(depEnd) {
			depEnd = end
			hasDepEnd = true
		}
	}
	return blockingIDs, depEnd, hasDepEnd, blocked
}

// recordPlacementEnd tracks the max end for dependency gating (pins +
// pinned already placed, plus new flexible blocks).
func recordPlacementEnd(plan *taskPlan, planBuilder *builder, ends map[string]time.Time) {
	max, ok := ends[plan.task.ID]
	for _, blk := range planBuilder.blocks {
		if blk.TaskID != plan.task.ID {
			continue
		}
		if !ok || blk.End.After(max) {
			max = blk.End
			ok = true
		}
	}
	if ok {
		ends[plan.task.ID] = max
	}
}

func buildReport(in reportInput) Report {
	inOrder := make(map[string]bool, len(in.order))
	for _, plan := range in.order {
		inOrder[plan.task.ID] = true
	}
	var report Report
	for _, plan := range in.candidates {
		if _, ok := in.blockedUnknown[plan.task.ID]; ok {
			ids := append([]string(nil), in.blockedUnknown[plan.task.ID]...)
			slices.Sort(ids)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    plan.task.ID,
				Required:  plan.n,
				Placed:    plan.placed,
				StoodDown: plan.stoodDown,
				Shortfall: plan.shortfall + plan.required(),
				Reason:    ReasonUnknownDependency,
				BlockedBy: ids,
			})
			continue
		}
		if in.cycleSet[plan.task.ID] {
			// Blocker: the cycle neighbours for context.
			var blockers []string
			for _, dep := range plan.task.Depends {
				if d, ok := in.lookup.lookup(dep); ok {
					blockers = append(blockers, d.task.ID)
				}
			}
			slices.Sort(blockers)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    plan.task.ID,
				Required:  plan.n,
				Placed:    plan.placed,
				StoodDown: plan.stoodDown,
				Shortfall: plan.n - plan.stoodDown - plan.pinnedSlots,
				Reason:    ReasonDependencyCycle,
				BlockedBy: blockers,
			})
			continue
		}
		if !inOrder[plan.task.ID] {
			// Downstream of a cycle: not a member, blocked by an unplaced
			// dependency. Name those dependencies for the report.
			var blockers []string
			for _, dep := range plan.task.Depends {
				d, ok := in.lookup.lookup(dep)
				if !ok || d.task.Container || d.task.Pin != nil || d.required() == 0 {
					continue
				}
				if !inOrder[d.task.ID] {
					blockers = append(blockers, d.task.ID)
				}
			}
			slices.Sort(blockers)
			blockers = slices.Compact(blockers)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    plan.task.ID,
				Required:  plan.n,
				Placed:    plan.placed,
				StoodDown: plan.stoodDown,
				Shortfall: plan.required(),
				Reason:    ReasonBlockedByDependency,
				BlockedBy: blockers,
			})
			continue
		}
	}
	for _, plan := range in.order {
		if r, ok := in.blockedReason[plan.task.ID]; ok {
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    plan.task.ID,
				Required:  plan.n,
				Placed:    plan.placed,
				StoodDown: plan.stoodDown,
				Shortfall: plan.shortfall,
				Reason:    r,
				BlockedBy: append([]string(nil), in.blockedBy[plan.task.ID]...),
			})
			continue
		}
		if plan.shortfall <= 0 {
			continue
		}
		reason := ReasonPartialFit
		if plan.placed == plan.pinnedSlots {
			reason = ReasonNoEligibleTime
		}
		var blockers []string
		if len(plan.task.Depends) > 0 {
			// Time-bound unmet warning: dependency pressure narrows the
			// window; name the deps so the warning is actionable (Q11).
			reason = ReasonBlockedByDependency
			blockers = append(blockers, plan.task.Depends...)
			slices.Sort(blockers)
		}
		report.Tasks = append(report.Tasks, TaskReport{
			TaskID:    plan.task.ID,
			Required:  plan.n,
			Placed:    plan.placed,
			StoodDown: plan.stoodDown,
			Shortfall: plan.shortfall,
			Reason:    reason,
			BlockedBy: blockers,
		})
	}
	// Pin-precedes-dependency violations: Pins stay, loudly reported (Q10).
	for _, plan := range in.pins {
		if len(plan.task.Depends) == 0 {
			continue
		}
		start := *plan.task.Pin
		var viol []string
		for _, dep := range plan.task.Depends {
			depPlan, ok := in.lookup.lookup(dep)
			if !ok {
				viol = append(viol, dep)
				continue
			}
			if depPlan.task.Container {
				continue
			}
			if end, ok := in.scope.ends[depPlan.task.ID]; ok {
				if start.Before(end) {
					viol = append(viol, depPlan.task.ID)
				}
				continue
			}
			// Dep never placed (blocked/cycle/beyond horizon): Pin
			// violates the order by definition.
			if depPlan.task.Pin == nil {
				viol = append(viol, depPlan.task.ID)
			}
		}
		if len(viol) > 0 {
			slices.Sort(viol)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    plan.task.ID,
				Required:  plan.n,
				Placed:    plan.placed,
				StoodDown: plan.stoodDown,
				Shortfall: 0,
				Reason:    ReasonPinPrecedesDependency,
				BlockedBy: viol,
			})
		}
	}

	return report
}
