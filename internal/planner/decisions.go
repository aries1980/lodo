package planner

import (
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// decisionsInput bundles buildDecisions' inputs so the signature stays readable.
type decisionsInput struct {
	plans          []*taskPlan
	blocks         []model.Block
	explanations   []Explanation
	order          []*taskPlan
	pins           []*taskPlan
	cycleSet       map[string]bool
	blockedUnknown map[string][]string
	blockedReason  map[string]Reason
	blockedBy      map[string][]string
	report         Report
	scope          *placementScope
}

func buildDecisions(in decisionsInput) []TaskDecision {
	inOrder := make(map[string]bool, len(in.order))
	for _, plan := range in.order {
		inOrder[plan.task.ID] = true
	}
	blocksByTask := map[string][]model.Block{}
	for _, blk := range in.blocks {
		blocksByTask[blk.TaskID] = append(blocksByTask[blk.TaskID], blk)
	}
	explByTaskIndex := map[string]Explanation{}
	for _, expl := range in.explanations {
		explByTaskIndex[norm.BlockKey(expl.TaskID, expl.BlockIndex)] = expl
	}
	reportByTask := map[string]TaskReport{}
	for _, rep := range in.report.Tasks {
		reportByTask[rep.TaskID] = rep
	}

	for _, plan := range in.plans {
		markEligibility(plan, inOrder, in.cycleSet, in.blockedUnknown, in.blockedReason, in.blockedBy)
		if in.scope.horizonEnd.Year != 0 && plan.decision.Window.HorizonEnd == "" {
			plan.decision.Window.HorizonEnd = in.scope.horizonEnd.String()
		}

		for _, blk := range blocksByTask[plan.task.ID] {
			block := BlockDecision{Index: blk.Index, Kind: blk.Kind, Start: blk.Start, End: blk.End, Slots: blk.Slots}
			if expl, ok := explByTaskIndex[norm.BlockKey(blk.TaskID, blk.Index)]; ok {
				block.RunStart = expl.RunStart
				block.RunSlots = expl.RunSlots
			}
			plan.decision.Blocks = append(plan.decision.Blocks, block)
		}

		// Outcome: prefer the actual report (authoritative, no drift) when present.
		outcome := Outcome{Required: plan.n, Placed: plan.placed, StoodDown: plan.stoodDown, Shortfall: plan.n - plan.stoodDown - plan.placed}
		if outcome.Shortfall < 0 {
			outcome.Shortfall = 0
		}
		if reported, ok := reportByTask[plan.task.ID]; ok {
			outcome.Required = reported.Required
			outcome.Placed = reported.Placed
			outcome.StoodDown = reported.StoodDown
			outcome.Shortfall = reported.Shortfall
			outcome.Reason = string(reported.Reason)
			outcome.BlockedBy = append([]string(nil), reported.BlockedBy...)
			if reported.Reason != "" {
				plan.decision.Dependency.BlockReason = string(reported.Reason)
				if len(reported.BlockedBy) > 0 {
					plan.decision.Dependency.BlockedBy = append([]string(nil), reported.BlockedBy...)
					plan.decision.Dependency.Blocked = true
				}
				if reported.Reason == ReasonPinPrecedesDependency {
					plan.decision.Dependency.PinViolation = true
				}
			}
		}
		plan.decision.Outcome = outcome
	}

	decisions := make([]TaskDecision, 0, len(in.plans))
	for _, plan := range in.plans {
		decisions = append(decisions, plan.decision)
	}
	slices.SortFunc(decisions, func(a, b TaskDecision) int {
		if a.Graph != b.Graph {
			return strings.Compare(a.Graph, b.Graph)
		}
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return 0
	})
	return decisions
}

// markEligibility records why a plan entered (or skipped) flexible placement.
func markEligibility(plan *taskPlan, inOrder map[string]bool, cycleSet map[string]bool, blockedUnknown map[string][]string, blockedReason map[string]Reason, blockedBy map[string][]string) {
	if plan.task.Pin != nil {
		plan.decision.Eligibility = Eligibility{Pin: true, RequiredSlots: plan.n, PinnedSlots: plan.n, FlexibleSlots: 0, Considered: true}
		plan.decision.Ordering.Kind = "pin"
	} else if plan.task.Container {
		plan.decision.Eligibility = Eligibility{Container: true, RequiredSlots: 0, FlexibleSlots: 0, Considered: false, SkipReason: "container: never scheduled"}
	} else if ids, ok := blockedUnknown[plan.task.ID]; ok {
		plan.decision.Eligibility.Considered = false
		plan.decision.Eligibility.SkipReason = "blocked by unknown dependency"
		plan.decision.Dependency.Blocked = true
		plan.decision.Dependency.BlockReason = string(ReasonUnknownDependency)
		ids = append([]string(nil), ids...)
		slices.Sort(ids)
		plan.decision.Dependency.BlockedBy = ids
	} else if cycleSet[plan.task.ID] {
		plan.decision.Eligibility.Considered = false
		plan.decision.Eligibility.SkipReason = "dependency cycle"
		plan.decision.Dependency.Blocked = true
		plan.decision.Dependency.BlockReason = string(ReasonDependencyCycle)
		plan.decision.Dependency.CycleMember = true
	} else if !inOrder[plan.task.ID] && plan.task.Pin == nil && !plan.task.Container && plan.required() > 0 {
		plan.decision.Eligibility.Considered = false
		if !plan.decision.Dependency.Blocked {
			plan.decision.Dependency.Blocked = true
			plan.decision.Dependency.BlockReason = string(ReasonBlockedByDependency)
		}
		if plan.decision.Eligibility.SkipReason == "" {
			plan.decision.Eligibility.SkipReason = "blocked by dependency"
		}
	} else if plan.required() == 0 && plan.task.Pin == nil && !plan.task.Container {
		plan.decision.Eligibility.Considered = false
		if plan.decision.Eligibility.SkipReason == "" {
			plan.decision.Eligibility.SkipReason = "fully satisfied by pinned/stood-down dispositions"
		}
	}
	if r, ok := blockedReason[plan.task.ID]; ok {
		plan.decision.Dependency.Blocked = true
		plan.decision.Dependency.BlockReason = string(r)
		plan.decision.Dependency.BlockedBy = append([]string(nil), blockedBy[plan.task.ID]...)
	}
}
