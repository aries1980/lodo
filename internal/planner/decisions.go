package planner

import (
	"cmp"
	"slices"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// decisionState is the terminal explain state one Task reached. The order of
// the constants is the precedence buildDecisions resolves: a Pin is also a
// Task, a container never schedules, and a Task outside the placement order
// is either blocked or already satisfied.
type decisionState uint8

const (
	statePinned decisionState = iota
	stateContainer
	stateUnknown
	stateCycle
	stateBeyondHorizon
	stateBlocked
	statePlaced
	stateDownstream
	stateSatisfied
)

// buildDecisions is the single writer of the explain trace: every TaskDecision
// field is assembled here from placement facts, order, and the shortfall
// report. The trace never feeds placement back (ADR 0006).
func (r *planRun) buildDecisions() []TaskDecision {
	blocksByTask := make(map[string][]model.Block, len(r.plans))
	for _, blk := range r.blocks {
		blocksByTask[blk.TaskID] = append(blocksByTask[blk.TaskID], blk)
	}
	explByBlock := make(map[string]Explanation, len(r.expls))
	for _, expl := range r.expls {
		explByBlock[norm.BlockKey(expl.TaskID, expl.BlockIndex)] = expl
	}
	reportByTask := make(map[string]*TaskReport, len(r.report.Tasks))
	for i := range r.report.Tasks {
		reportByTask[r.report.Tasks[i].TaskID] = &r.report.Tasks[i]
	}

	decisions := make([]TaskDecision, 0, len(r.plans))
	for _, plan := range r.plans {
		decisions = append(decisions, decisionFor(plan, r, blocksByTask[plan.task.ID], explByBlock, reportByTask[plan.task.ID]))
	}
	slices.SortFunc(decisions, func(a, b TaskDecision) int {
		return cmp.Or(cmp.Compare(a.Graph, b.Graph), cmp.Compare(a.TaskID, b.TaskID))
	})
	return decisions
}

func decisionFor(plan *taskPlan, r *planRun, blocks []model.Block, expls map[string]Explanation, reported *TaskReport) TaskDecision {
	return TaskDecision{
		TaskID:       plan.task.ID,
		Text:         plan.task.Text,
		Graph:        plan.task.Graph,
		Eligibility:  eligibilityFor(plan, r),
		Ordering:     orderingFor(plan, r),
		Dependency:   dependencyFor(plan, r, reported),
		Window:       windowFor(plan, r),
		TimeMaps:     timeMapsFor(plan),
		Placement:    placementFor(plan),
		Blocks:       blocksFor(blocks, expls),
		Outcome:      outcomeFor(plan, reported),
		Dispositions: dispositionsFor(plan),
	}
}

// stateOf resolves the terminal state in the same precedence the planner
// applies: Pin, container, unknown dependency, cycle, beyond horizon, the
// dependency-gated order, then downstream or satisfied.
func stateOf(plan *taskPlan, r *planRun) decisionState {
	switch {
	case plan.task.Pin != nil:
		return statePinned
	case plan.task.Container:
		return stateContainer
	case len(r.unknown[plan.task.ID]) > 0:
		return stateUnknown
	case r.cycleSet.Has(plan.task.ID):
		return stateCycle
	case plan.facts.beyondHorizon:
		return stateBeyondHorizon
	case r.orderSet.Has(plan.task.ID):
		if _, blocked := r.blocked[plan.task.ID]; blocked {
			return stateBlocked
		}
		return statePlaced
	case plan.required() > 0:
		return stateDownstream
	default:
		return stateSatisfied
	}
}

// eligibilityFor is the one place an Eligibility is built. Pin and container
// are special cases of the same base counts: a Pin has all its slots pinned,
// a container has none.
func eligibilityFor(plan *taskPlan, r *planRun) Eligibility {
	e := Eligibility{
		RequiredSlots:  plan.n,
		StoodDownSlots: plan.stoodDown,
		PinnedSlots:    plan.pinnedSlots,
		FlexibleSlots:  plan.required(),
	}
	switch stateOf(plan, r) {
	case statePinned:
		e.Pin = true
		e.Considered = true
	case stateContainer:
		e.Container = true
		e.SkipReason = "container: never scheduled"
	case stateUnknown:
		e.SkipReason = "blocked by unknown dependency"
	case stateCycle:
		e.SkipReason = "dependency cycle"
	case stateBeyondHorizon:
		e.BeyondHorizon = true
		e.SkipReason = "beyond scheduling horizon: not due yet"
	case stateBlocked, statePlaced:
		e.Considered = true
	case stateDownstream:
		e.SkipReason = "blocked by dependency"
	case stateSatisfied:
		e.SkipReason = "fully satisfied by pinned/stood-down dispositions"
	}
	return e
}

// orderingFor is the one place Ordering is built; Total is the consideration
// order size for every Task, considered or not.
func orderingFor(plan *taskPlan, r *planRun) Ordering {
	o := Ordering{
		GraphOrder:   plan.task.GraphOrder,
		Priority:     string(plan.task.Priority),
		PriorityRank: plan.task.Priority.Rank(),
		OutlineOrder: plan.task.OutlineOrder,
		Position:     plan.facts.position,
		Total:        len(r.order),
	}
	if plan.task.PlanEnd != nil {
		o.PlanEnd = plan.task.PlanEnd.String()
	}

	switch stateOf(plan, r) {
	case statePinned:
		o.Kind = "pin"
		o.TieBreak = "pin-first"
		o.TieBreakDetail = "pins are placed first at their exact time; everything else schedules around them"
	case stateContainer:
		o.Kind = "container"
		o.TieBreak = "never-scheduled"
		o.TieBreakDetail = "containers never enter consideration order; only their leaves are scheduled"
	case stateUnknown:
		o.Kind = "blocked"
		o.TieBreak = "blocked"
		o.TieBreakDetail = "unknown dependency: depends:: references no task"
	case stateCycle:
		o.Kind = "cycle-blocked"
		o.TieBreak = "dependency-cycle"
		o.TieBreakDetail = "dependency cycle: tasks in the cycle are left unscheduled"
	case stateBeyondHorizon:
		o.Kind = kindFor(plan)
		o.TieBreak = "beyond-horizon"
		o.TieBreakDetail = "not due yet: plan-start is on or after the horizon end; left unplaced silently until it enters the window"
	case stateBlocked, statePlaced:
		o.Kind = kindFor(plan)
		o.TieBreak = plan.facts.tieBreak
		o.TieBreakDetail = plan.facts.tieBreakDetail
	case stateDownstream:
		o.Kind = "blocked"
		o.TieBreak = "dependency"
		o.TieBreakDetail = "blocked by unplaced dependency; downstream of a cycle or blocked task"
	case stateSatisfied:
		o.Kind = kindFor(plan)
		o.TieBreak = "satisfied"
		o.TieBreakDetail = "fully satisfied by pinned/stood-down dispositions; no flexible hours remain"
	}
	return o
}

func kindFor(plan *taskPlan) string {
	if plan.task.PlanStart != nil || plan.task.PlanEnd != nil {
		return "time-bound"
	}
	return "flexible"
}

// dependencyFor is the one place Dependency is built. A report entry is
// authoritative for block reason, blockers and Pin violations.
func dependencyFor(plan *taskPlan, r *planRun, reported *TaskReport) Dependency {
	d := Dependency{Depends: append([]string(nil), plan.task.Depends...)}
	if plan.facts.hasLowerBound {
		d.LowerBound = plan.facts.lowerBound
	}
	if r.cycleSet.Has(plan.task.ID) {
		d.CycleMember = true
	}

	switch stateOf(plan, r) {
	case stateUnknown:
		ids := append([]string(nil), r.unknown[plan.task.ID]...)
		slices.Sort(ids)
		d.Blocked = true
		d.BlockReason = string(ReasonUnknownDependency)
		d.BlockedBy = ids
	case stateCycle:
		d.Blocked = true
		d.BlockReason = string(ReasonDependencyCycle)
	case stateDownstream:
		d.Blocked = true
		d.BlockReason = string(ReasonBlockedByDependency)
	case stateBlocked:
		d.Blocked = true
		d.BlockReason = string(r.blocked[plan.task.ID])
		d.BlockedBy = append([]string(nil), r.blockedBy[plan.task.ID]...)
	}

	if reported != nil && reported.Reason != "" {
		d.BlockReason = string(reported.Reason)
		if len(reported.BlockedBy) > 0 {
			d.Blocked = true
			d.BlockedBy = append([]string(nil), reported.BlockedBy...)
		}
		if reported.Reason == ReasonPinPrecedesDependency {
			d.PinViolation = true
		}
	}
	return d
}

// windowFor is the one place Window is built. The effective from/to/bound and
// overdue flag exist only when the flexible search actually ran.
func windowFor(plan *taskPlan, r *planRun) Window {
	w := Window{HorizonEnd: r.horizonEnd.String()}
	if plan.task.PlanStart != nil {
		w.PlanStart = plan.task.PlanStart.String()
	}
	if plan.task.PlanEnd != nil {
		w.PlanEnd = plan.task.PlanEnd.String()
	}
	if search := plan.facts.search; search != nil {
		w.Overdue = search.window.overdue
		w.From = search.window.from
		w.To = search.window.to
		w.Bound = search.window.bound
	}
	return w
}

func timeMapsFor(plan *taskPlan) TimeMaps {
	if plan.facts.search == nil {
		return TimeMaps{}
	}
	return plan.facts.search.timeMaps
}

func placementFor(plan *taskPlan) *Placement {
	if plan.facts.search == nil {
		return nil
	}
	return &plan.facts.search.placement
}

func blocksFor(blocks []model.Block, expls map[string]Explanation) []BlockDecision {
	var out []BlockDecision
	for _, blk := range blocks {
		block := BlockDecision{Index: blk.Index, Kind: blk.Kind, Start: blk.Start, End: blk.End, Slots: blk.Slots}
		if expl, ok := expls[norm.BlockKey(blk.TaskID, blk.Index)]; ok {
			block.RunStart = expl.RunStart
			block.RunSlots = expl.RunSlots
		}
		out = append(out, block)
	}
	return out
}

func outcomeFor(plan *taskPlan, reported *TaskReport) Outcome {
	if reported != nil {
		return Outcome{
			Required:  reported.Required,
			Placed:    reported.Placed,
			StoodDown: reported.StoodDown,
			Shortfall: reported.Shortfall,
			Reason:    string(reported.Reason),
			BlockedBy: append([]string(nil), reported.BlockedBy...),
		}
	}
	shortfall := plan.n - plan.stoodDown - plan.placed
	if shortfall < 0 {
		shortfall = 0
	}
	return Outcome{Required: plan.n, Placed: plan.placed, StoodDown: plan.stoodDown, Shortfall: shortfall}
}

func dispositionsFor(plan *taskPlan) []model.Disposition {
	var disps []model.Disposition
	disps = append(disps, plan.pinnedDisps...)
	disps = append(disps, plan.stoodDownDisps...)
	slices.SortFunc(disps, func(a, b model.Disposition) int {
		return cmp.Or(cmp.Compare(a.BlockIndex, b.BlockIndex), cmp.Compare(a.Kind, b.Kind))
	})
	if len(disps) == 0 {
		return nil
	}
	return disps
}
