package planner

import (
	"fmt"
	"time"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

func buildPlans(tasks []model.Task, dispositions map[string][]model.Disposition, granularity time.Duration) ([]*taskPlan, error) {
	plans := make([]*taskPlan, len(tasks))
	for i, task := range tasks {
		if task.Container {
			plans[i] = newTaskPlan(task, nil, granularity)
			plans[i].n = 0
			plans[i].placed = 0
			plans[i].shortfall = 0
			plans[i].stoodDown = 0
			plans[i].pinnedSlots = 0
			plans[i].decision.Eligibility.Container = true
			plans[i].decision.Eligibility.Considered = false
			plans[i].decision.Eligibility.SkipReason = "container: never scheduled"
			continue
		}
		if task.Duration <= 0 {
			return nil, fmt.Errorf("planner: task %s: duration must be positive", task.ID)
		}
		plans[i] = newTaskPlan(task, dispositions[task.ID], granularity)
	}
	return plans, nil
}

// taskPlan accumulates one Task's placement state.
//
// Invariant: n is the whole duration in granularity units; required =
// n - stoodDown - pinnedSlots is what flexible placement must still place.
type taskPlan struct {
	task           model.Task
	n              int
	stoodDown      int
	pinnedSlots    int
	placed         int
	shortfall      int
	pinnedDisps    []model.Disposition
	stoodDownDisps []model.Disposition
	reserved       map[int]bool
	decision       TaskDecision
}

func newTaskPlan(task model.Task, dispositions []model.Disposition, granularity time.Duration) *taskPlan {
	plan := &taskPlan{task: task, n: norm.Units(task.Duration, granularity), reserved: make(map[int]bool)}
	plan.decision = TaskDecision{
		TaskID: task.ID,
		Text:   task.Text,
		Graph:  task.Graph,
		Ordering: Ordering{
			GraphOrder:   task.GraphOrder,
			Priority:     string(task.Priority),
			PriorityRank: task.Priority.Rank(),
			OutlineOrder: task.OutlineOrder,
		},
		Dependency: Dependency{Depends: append([]string(nil), task.Depends...)},
	}
	if task.PlanStart != nil {
		plan.decision.Window.PlanStart = task.PlanStart.String()
	}
	if task.PlanEnd != nil {
		plan.decision.Window.PlanEnd = task.PlanEnd.String()
		plan.decision.Ordering.PlanEnd = task.PlanEnd.String()
	}

	stoodDown := 0
	pinned := 0
	for _, disposition := range dispositions {
		plan.reserved[disposition.BlockIndex] = true
		switch disposition.Kind {
		case model.DispositionPinned:
			plan.pinnedDisps = append(plan.pinnedDisps, disposition)
			pinned += norm.Units(disposition.End.Sub(disposition.Start), granularity)
		case model.DispositionStoodDown:
			plan.stoodDownDisps = append(plan.stoodDownDisps, disposition)
			stoodDown += disposition.Slots
		}
	}

	if task.Pin != nil {
		// A Pin overrides Plan, Priority and any recorded dispositions.
		plan.pinnedDisps = nil
		plan.stoodDownDisps = nil
		plan.reserved = map[int]bool{}
		plan.pinnedSlots = plan.n
		plan.placed = plan.n
		plan.decision.Eligibility = Eligibility{Pin: true, RequiredSlots: plan.n, PinnedSlots: plan.n, FlexibleSlots: 0, Considered: true}
		plan.decision.Ordering.Kind = "pin"
		return plan
	}

	plan.stoodDown = min(stoodDown, plan.n)
	plan.pinnedSlots = min(pinned, plan.n-plan.stoodDown)
	plan.placed = plan.pinnedSlots
	plan.decision.Eligibility = Eligibility{
		RequiredSlots:  plan.n,
		StoodDownSlots: plan.stoodDown,
		PinnedSlots:    plan.pinnedSlots,
		FlexibleSlots:  plan.required(),
	}
	return plan
}

// required returns the units still to place flexibly.
func (plan *taskPlan) required() int { return plan.n - plan.stoodDown - plan.pinnedSlots }

func indexDispositions(dispositions []model.Disposition) (map[string][]model.Disposition, error) {
	out := make(map[string][]model.Disposition)
	type key struct {
		task  string
		block int
	}
	seen := make(map[key]bool)
	for _, disposition := range dispositions {
		switch disposition.Kind {
		case model.DispositionPinned:
			if !disposition.Start.Before(disposition.End) {
				return nil, fmt.Errorf("planner: task %s block %d: pinned disposition has an empty span", disposition.TaskID, disposition.BlockIndex)
			}
		case model.DispositionStoodDown:
			if disposition.Slots < 0 {
				return nil, fmt.Errorf("planner: task %s block %d: stood-down disposition has negative size", disposition.TaskID, disposition.BlockIndex)
			}
		default:
			return nil, fmt.Errorf("planner: task %s block %d: unknown disposition kind %q", disposition.TaskID, disposition.BlockIndex, disposition.Kind)
		}
		if disposition.BlockIndex < 0 {
			return nil, fmt.Errorf("planner: task %s: negative block index %d", disposition.TaskID, disposition.BlockIndex)
		}
		seenKey := key{task: disposition.TaskID, block: disposition.BlockIndex}
		if seen[seenKey] {
			return nil, fmt.Errorf("planner: task %s: more than one disposition for block %d", disposition.TaskID, disposition.BlockIndex)
		}
		seen[seenKey] = true
		out[disposition.TaskID] = append(out[disposition.TaskID], disposition)
	}
	return out, nil
}
