package planner

import (
	"fmt"
	"time"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/set"
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
	reserved       set.Set[int]
	facts          planFacts
}

// planFacts is what placement records for the explain trace. Placement never
// reads it back; buildDecisions is its only consumer (ADR 0006).
type planFacts struct {
	tieBreak       string
	tieBreakDetail string
	position       int
	beyondHorizon  bool
	lowerBound     time.Time
	hasLowerBound  bool
	search         *searchResult
}

// searchResult is what one flexible placement search saw and chose.
type searchResult struct {
	window    searchWindow
	placement Placement
	timeMaps  TimeMaps
}

// searchWindow is the effective placement window for one Task.
type searchWindow struct {
	overdue bool
	from    time.Time
	to      time.Time
	bound   time.Time
}

func (plan *taskPlan) markTieBreak(key, detail string) {
	plan.facts.tieBreak = key
	plan.facts.tieBreakDetail = detail
}

func (plan *taskPlan) markPosition(position int) { plan.facts.position = position }

func (plan *taskPlan) markBeyondHorizon() { plan.facts.beyondHorizon = true }

func (plan *taskPlan) markLowerBound(end time.Time) {
	plan.facts.lowerBound = end
	plan.facts.hasLowerBound = true
}

func (plan *taskPlan) markSearch(result searchResult) { plan.facts.search = &result }

func newTaskPlan(task model.Task, dispositions []model.Disposition, granularity time.Duration) *taskPlan {
	plan := &taskPlan{task: task, n: norm.Units(task.Duration, granularity), reserved: set.New[int]()}

	stoodDown := 0
	pinned := 0
	for _, disposition := range dispositions {
		plan.reserved.Add(disposition.BlockIndex)
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
		clear(plan.reserved)
		plan.pinnedSlots = plan.n
		plan.placed = plan.n
		return plan
	}

	plan.stoodDown = min(stoodDown, plan.n)
	plan.pinnedSlots = min(pinned, plan.n-plan.stoodDown)
	plan.placed = plan.pinnedSlots
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
	seen := set.New[key]()
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
		if !seen.Add(seenKey) {
			return nil, fmt.Errorf("planner: task %s: more than one disposition for block %d", disposition.TaskID, disposition.BlockIndex)
		}
		out[disposition.TaskID] = append(out[disposition.TaskID], disposition)
	}
	return out, nil
}
