// Package planner turns resolved Tasks into a Schedule.
//
// Plan is a pure function: its only inputs are the ones passed in, it reads no
// files, makes no network calls and never looks at the wall clock, and
// identical inputs always produce identical output (ADR 0006).
//
// Files, in pipeline order:
//   - plan.go — Plan, Input/Schedule, and the pin helpers
//   - run.go — planRun, the state one Plan call threads through its phases
//   - taskplan.go — one Task's placement state and explain facts
//   - order.go — dependency order (topoOrder)
//   - search.go — free-run search and Block emission
//   - comparator.go — the ADR 0010 placement comparator
//   - report.go — the dependency gate (placeOrdered) and the shortfall report
//   - decisions.go — the single explain-decision assembler
//   - trace.go — explain decision types
//   - suggest.go — advisory suggestions for shortfalls
package planner

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// Input is everything the planner is allowed to look at.
//
// Invariant: Plan reads only these fields, never files, network or wall
// clock, so identical Inputs always produce identical Schedules (ADR 0006).
// Busy holds the busy time already projected onto the same grid the planner
// will build (nil means no busy time); Dispositions must contain only the
// dispositions that still apply, since deciding that is reconciliation's
// job, not the planner's.
type Input struct {
	Tasks        []model.Task
	TimeMaps     []model.TimeMap
	Busy         grid.Mask
	Dispositions []model.Disposition
	Now          time.Time
	Granularity  time.Duration
	WindowDays   int
	Location     *time.Location
}

// Schedule is the planner's output.
type Schedule struct {
	Blocks       []model.Block
	Report       Report
	Explanations []Explanation
	Decisions    []TaskDecision
	Suggestions  []Suggestion
}

// Report lists the Tasks the planner could not fully place.
type Report struct {
	Tasks []TaskReport
}

// TaskReport is one Task's outcome. placed + shortfall + stoodDown = n, with n
// the Task's whole duration in granularity units.
type TaskReport struct {
	TaskID    string
	Required  int
	Placed    int
	StoodDown int
	Shortfall int
	Reason    Reason
	// BlockedBy names the dependency UUIDs that blocked placement, when the
	// reason is dependency-related.
	BlockedBy []string
}

// Reason explains a shortfall.
//
// The string values are stable user-facing sentences: they appear in
// --json reports and explain output, so changing them breaks consumers.
// Use Code for short greppable keys in code, logs and tests.
type Reason string

const (
	ReasonNoEligibleTime        Reason = "no eligible time in the Scheduling window"
	ReasonPartialFit            Reason = "not enough eligible time to place the whole Task"
	ReasonBlockedByDependency   Reason = "blocked by incomplete dependencies"
	ReasonDependencyCycle       Reason = "dependency cycle: Tasks in the cycle are left unscheduled"
	ReasonUnknownDependency     Reason = "unknown dependency: depends:: references no Task"
	ReasonPinPrecedesDependency Reason = "pin precedes dependency: the Pin stays, the order is violated"
)

// Code returns the short machine key for a Reason.
func (r Reason) Code() string {
	switch r {
	case ReasonNoEligibleTime:
		return "no-eligible-time"
	case ReasonPartialFit:
		return "partial-fit"
	case ReasonBlockedByDependency:
		return "blocked-by-dependency"
	case ReasonDependencyCycle:
		return "dependency-cycle"
	case ReasonUnknownDependency:
		return "unknown-dependency"
	case ReasonPinPrecedesDependency:
		return "pin-precedes-dependency"
	default:
		return string(r)
	}
}

// Human returns the user-facing sentence for a Reason.
func (r Reason) Human() string { return string(r) }

// planLookup indexes placeable plans by exact and case-folded ID so
// depends:: resolution is O(1). It replaces the byID/lowerByID pair that
// previously travelled through every ordering/reporting signature.
type planLookup struct {
	byID      map[string]*taskPlan
	lowerByID map[string]*taskPlan
}

// lookup resolves one depends:: entry: exact ID first, then
// case-insensitive match. ok is false for dangling refs, which block.
func (l planLookup) lookup(dep string) (depPlan *taskPlan, ok bool) {
	if d, found := l.byID[dep]; found {
		return d, true
	}
	if d, found := l.lowerByID[norm.ID(dep)]; found {
		return d, true
	}
	return nil, false
}

// Explanation records where one Block came from, for `forecast` and
// `schedule --verbose`. RunStart and RunSlots describe the free run a flexible
// Block was placed in; they are zero for Pins and Pinned Blocks.
type Explanation struct {
	TaskID     string
	BlockIndex int
	Kind       model.BlockKind
	Start      time.Time
	Slots      int
	RunStart   time.Time
	RunSlots   int
}

func Plan(in Input) (Schedule, error) {
	if err := validateInput(in); err != nil {
		return Schedule{}, err
	}

	run, err := newPlanRun(in)
	if err != nil {
		return Schedule{}, err
	}

	// Pins are immovable and exact, so they shape the timeline before any
	// search begins. Then order the search, gate placement on dependencies,
	// report shortfalls, and assemble the explain trace from the facts.
	run.emitPins()
	run.orderCandidates()
	run.placeOrdered()
	return run.schedule(), nil
}

func validateInput(in Input) error {
	if in.Location == nil {
		return fmt.Errorf("planner: location is required")
	}
	if in.Granularity <= 0 || in.Granularity > 24*time.Hour {
		return fmt.Errorf("planner: granularity must be within (0h, 24h], got %v", in.Granularity)
	}
	if in.WindowDays <= 0 {
		return fmt.Errorf("planner: window days must be positive, got %d", in.WindowDays)
	}
	return nil
}

func buildBusy(busy grid.Mask, g *grid.Grid) (grid.Mask, error) {
	out := grid.NewMask(g.Len())
	if len(busy) > 0 {
		if len(busy) != g.MaskWords() {
			return nil, fmt.Errorf("planner: busy mask has %d words, want %d", len(busy), g.MaskWords())
		}
		out = busy.Clone()
	}
	return out, nil
}

func collectPins(plans []*taskPlan) []*taskPlan {
	pins := make([]*taskPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.task.Pin != nil && !plan.task.Container {
			pins = append(pins, plan)
		}
	}
	slices.SortFunc(pins, func(a, b *taskPlan) int {
		return cmp.Or(a.task.Pin.Compare(*b.task.Pin), compareTasks(a.task, b.task))
	})
	return pins
}

func emitPins(pins []*taskPlan, planBuilder *builder) {
	for _, plan := range pins {
		start := *plan.task.Pin
		planBuilder.addExact(model.Block{
			TaskID: plan.task.ID,
			Index:  0,
			Start:  start,
			End:    start.Add(plan.task.Duration),
			Slots:  plan.n,
			Kind:   model.BlockPin,
		})
	}
}

// emitPinned keeps the user-moved spans and marks every slot they overlap
// busy, so nothing else may overlap them.
func emitPinned(plans []*taskPlan, planBuilder *builder, granularity time.Duration) {
	for _, plan := range plans {
		if plan.task.Container {
			continue
		}
		slices.SortFunc(plan.pinnedDisps, func(a, b model.Disposition) int {
			return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.BlockIndex, b.BlockIndex))
		})
		for _, d := range plan.pinnedDisps {
			planBuilder.addExact(model.Block{
				TaskID: plan.task.ID,
				Index:  d.BlockIndex,
				Start:  d.Start,
				End:    d.End,
				Slots:  norm.Units(d.End.Sub(d.Start), granularity),
				Kind:   model.BlockPinned,
			})
		}
	}
}

func indexPlans(plans []*taskPlan) planLookup {
	byID := make(map[string]*taskPlan, len(plans))
	for _, plan := range plans {
		if plan.task.Container {
			continue
		}
		byID[plan.task.ID] = plan
	}
	lowerByID := make(map[string]*taskPlan, len(byID))
	for id, plan := range byID {
		lowerByID[norm.ID(id)] = plan
	}
	return planLookup{byID: byID, lowerByID: lowerByID}
}

// collectCandidates partitions placeable non-Pin Tasks; Pins are already placed.
func collectCandidates(plans []*taskPlan) []*taskPlan {
	var candidates []*taskPlan
	for _, plan := range plans {
		if plan.task.Container || plan.task.Pin != nil || plan.required() == 0 {
			continue
		}
		candidates = append(candidates, plan)
	}
	return candidates
}

// findUnknownDeps collects dangling (unknown) dependencies, which block immediately.
func findUnknownDeps(candidates []*taskPlan, lookup planLookup) map[string][]string {
	blockedUnknown := map[string][]string{}
	for _, plan := range candidates {
		for _, dep := range plan.task.Depends {
			if _, ok := lookup.lookup(dep); ok {
				continue
			}
			blockedUnknown[plan.task.ID] = append(blockedUnknown[plan.task.ID], dep)
		}
	}
	return blockedUnknown
}

func initialEnds(pins, plans []*taskPlan) map[string]time.Time {
	ends := map[string]time.Time{}
	for _, plan := range pins {
		ends[plan.task.ID] = plan.task.Pin.Add(plan.task.Duration)
	}
	for _, plan := range plans {
		if plan.task.Container {
			continue
		}
		if len(plan.pinnedDisps) > 0 {
			max := ends[plan.task.ID]
			found := false
			for _, d := range plan.pinnedDisps {
				if !found || d.End.After(max) {
					max = d.End
					found = true
				}
			}
			if found {
				ends[plan.task.ID] = max
			}
		}
	}
	return ends
}

func indexTimeMaps(maps []model.TimeMap) map[string]model.TimeMap {
	out := make(map[string]model.TimeMap, len(maps))
	for _, timeMap := range maps {
		out[timeMap.Slug] = timeMap
	}
	return out
}
