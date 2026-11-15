package planner

import (
	"slices"
	"time"
)

// placeOrdered gates each Task on its dependencies and places the
// survivors. Any dependency that is stood down, short, unplaced, or in a
// cycle blocks the Task entirely. It records ends as the max of pinned and
// newly placed flexible blocks.
func (r *planRun) placeOrdered() {
	r.blocked = map[string]Reason{}
	r.blockedBy = map[string][]string{}

	for _, plan := range r.order {
		if r.beyondHorizon(plan) {
			continue // not due yet; left unplaced silently
		}
		// Full-gate: any dep stood-down, shortfall, unplaced, or cycle
		// blocks the Task entirely.
		blockingIDs, depEnd, hasDepEnd, blocked := r.dependencyGate(plan)
		if hasDepEnd {
			plan.markLowerBound(depEnd)
		}
		if blocked {
			plan.shortfall = plan.required()
			slices.Sort(blockingIDs)
			r.blocked[plan.task.ID] = ReasonBlockedByDependency
			r.blockedBy[plan.task.ID] = blockingIDs
			continue
		}
		startBound := r.in.Now
		if hasDepEnd && depEnd.After(startBound) {
			startBound = depEnd
		}
		window := r.searchWindow(plan, startBound)
		plan.markSearch(r.b.placeWithBound(plan, r.maps, window))
		r.recordPlacementEnd(plan)
	}
}

// beyondHorizon reports whether the Task is not due yet and records that fact
// for explain. Pins and containers never reach this gate.
func (r *planRun) beyondHorizon(plan *taskPlan) bool {
	if plan.task.PlanStart == nil || plan.task.PlanStart.Before(r.horizonEnd) {
		return false
	}
	plan.markBeyondHorizon()
	return true
}

// dependencyGate checks finish-to-start predecessors: explicit depends plus
// sequential-chain edges already expanded into Depends.
func (r *planRun) dependencyGate(plan *taskPlan) (blockingIDs []string, depEnd time.Time, hasDepEnd bool, blocked bool) {
	for _, dep := range plan.task.Depends {
		depPlan, ok := r.lookup.lookup(dep)
		if !ok {
			blockingIDs = append(blockingIDs, dep)
			blocked = true
			continue
		}
		if depPlan.task.Container {
			continue
		}
		if depPlan.task.Pin != nil {
			if end, ok := r.ends[depPlan.task.ID]; ok {
				if !hasDepEnd || end.After(depEnd) {
					depEnd = end
					hasDepEnd = true
				}
			}
			continue
		}
		if r.cycleSet.Has(depPlan.task.ID) {
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
		end, ok := r.ends[depPlan.task.ID]
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
func (r *planRun) recordPlacementEnd(plan *taskPlan) {
	max, ok := r.ends[plan.task.ID]
	for _, blk := range r.b.blocks {
		if blk.TaskID != plan.task.ID {
			continue
		}
		if !ok || blk.End.After(max) {
			max = blk.End
			ok = true
		}
	}
	if ok {
		r.ends[plan.task.ID] = max
	}
}

// reportFor is the single TaskReport constructor, so every branch names the
// same Task fields and only varies the reason, blockers and shortfall.
func reportFor(plan *taskPlan, reason Reason, blockers []string, shortfall int) TaskReport {
	return TaskReport{
		TaskID:    plan.task.ID,
		Required:  plan.n,
		Placed:    plan.placed,
		StoodDown: plan.stoodDown,
		Shortfall: shortfall,
		Reason:    reason,
		BlockedBy: blockers,
	}
}

func (r *planRun) buildReport() Report {
	var report Report
	for _, plan := range r.candidates {
		if _, ok := r.unknown[plan.task.ID]; ok {
			ids := append([]string(nil), r.unknown[plan.task.ID]...)
			slices.Sort(ids)
			report.Tasks = append(report.Tasks, reportFor(plan, ReasonUnknownDependency, ids, plan.required()))
			continue
		}
		if r.cycleSet.Has(plan.task.ID) {
			// Blocker: the cycle neighbours for context.
			var blockers []string
			for _, dep := range plan.task.Depends {
				if d, ok := r.lookup.lookup(dep); ok {
					blockers = append(blockers, d.task.ID)
				}
			}
			slices.Sort(blockers)
			report.Tasks = append(report.Tasks, reportFor(plan, ReasonDependencyCycle, blockers, plan.required()))
			continue
		}
		if !r.orderSet.Has(plan.task.ID) {
			// Downstream of a cycle: not a member, blocked by an unplaced
			// dependency. Name those dependencies for the report.
			var blockers []string
			for _, dep := range plan.task.Depends {
				d, ok := r.lookup.lookup(dep)
				if !ok || d.task.Container || d.task.Pin != nil || d.required() == 0 {
					continue
				}
				if !r.orderSet.Has(d.task.ID) {
					blockers = append(blockers, d.task.ID)
				}
			}
			slices.Sort(blockers)
			blockers = slices.Compact(blockers)
			report.Tasks = append(report.Tasks, reportFor(plan, ReasonBlockedByDependency, blockers, plan.required()))
			continue
		}
	}
	for _, plan := range r.order {
		if reason, ok := r.blocked[plan.task.ID]; ok {
			blockers := append([]string(nil), r.blockedBy[plan.task.ID]...)
			report.Tasks = append(report.Tasks, reportFor(plan, reason, blockers, plan.shortfall))
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
			// window; name the deps so the warning is actionable.
			reason = ReasonBlockedByDependency
			blockers = append(blockers, plan.task.Depends...)
			slices.Sort(blockers)
		}
		report.Tasks = append(report.Tasks, reportFor(plan, reason, blockers, plan.shortfall))
	}
	// Pin-precedes-dependency violations: Pins stay, and the order break is reported.
	for _, plan := range r.pins {
		if len(plan.task.Depends) == 0 {
			continue
		}
		start := *plan.task.Pin
		var viol []string
		for _, dep := range plan.task.Depends {
			depPlan, ok := r.lookup.lookup(dep)
			if !ok {
				viol = append(viol, dep)
				continue
			}
			if depPlan.task.Container {
				continue
			}
			if end, ok := r.ends[depPlan.task.ID]; ok {
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
			report.Tasks = append(report.Tasks, reportFor(plan, ReasonPinPrecedesDependency, viol, 0))
		}
	}

	return report
}
