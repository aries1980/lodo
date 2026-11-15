package planner

import (
	"cmp"
	"slices"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

// builder accumulates Blocks while blocking their slots on busy.
type builder struct {
	g      *grid.Grid
	busy   grid.Mask
	blocks []model.Block
	expls  []Explanation

	// Masks for explain breakdowns (all over the same grid): external
	// calendar busy at run start, pins, user-moved (pinned) blocks, and the
	// union of those three at the start of flexible placement. Placed-early
	// pressure is busy minus baseBusy.
	external   grid.Mask
	pinMask    grid.Mask
	pinnedMask grid.Mask
	baseBusy   grid.Mask
}

// addExact emits a Block at its exact time, never snapped, and marks every
// slot it overlaps busy.
func (b *builder) addExact(block model.Block) {
	b.blocks = append(b.blocks, block)
	b.expls = append(b.expls, Explanation{
		TaskID:     block.TaskID,
		BlockIndex: block.Index,
		Kind:       block.Kind,
		Start:      block.Start,
		Slots:      block.Slots,
	})
	b.busy.OrInto(b.g.Overlapping(block.Start, block.End))
}

// addFlexible emits a flexible Block and marks its slots busy.
func (b *builder) addFlexible(block model.Block, run grid.Run) {
	b.blocks = append(b.blocks, block)
	b.expls = append(b.expls, Explanation{
		TaskID:     block.TaskID,
		BlockIndex: block.Index,
		Kind:       block.Kind,
		Start:      block.Start,
		Slots:      block.Slots,
		RunStart:   b.g.Start(run.Start),
		RunSlots:   run.Slots,
	})
	b.busy.OrInto(b.g.Overlapping(block.Start, block.End))
}

// searchWindow resolves the window one placed Task may use: the Plan clip
// unless it is overdue, then the dependency/now bound.
func (r *planRun) searchWindow(plan *taskPlan, bound time.Time) searchWindow {
	from, to := r.g.WindowStart(), r.g.WindowEnd()
	overdue := overdue(plan.task, r.today)
	if !overdue {
		if plan.task.PlanStart != nil {
			from = plan.task.PlanStart.Midnight(r.g.Location())
		}
		if plan.task.PlanEnd != nil {
			to = plan.task.PlanEnd.AddDays(1).Midnight(r.g.Location())
		}
	}
	return searchWindow{overdue: overdue, from: from, to: to, bound: bound}
}

// placeWithBound searches the Task's free slots and emits its flexible Blocks.
// win carries the effective window and the dependency-aware lower bound: the
// Task may not start before it.
//
// Hours still required are placed in three tier passes — green, then yellow,
// then red — so a later green window beats an earlier yellow one. A Task that
// names its own Time Maps prefers those hours first and spills into the
// default map when its project map is full, but never upward: a fallback pass
// runs only for the task's best primary tier and worse, so a yellow-only
// project map spills into default yellow/red, never green. Each
// pass uses the ADR 0010 comparator on that tier's free runs. A Block never
// crosses a tier boundary. The minimum-Block floor applies per pass, so a
// one-slot green fragment is left unused and the hours can spill.
func (b *builder) placeWithBound(plan *taskPlan, maps map[string]model.TimeMap, win searchWindow) searchResult {
	window := b.g.Inside(win.from, win.to).And(b.g.NotBefore(win.bound))

	floor := 2
	if plan.n == 1 {
		floor = 1
	}

	left := plan.required()
	next := 0
	sets := newTimeMapSets(plan.task, maps)
	trace := sets.trace(plan.task.TimeMaps)
	state := &tierState{
		left: &left, next: &next, trace: &trace, window: window,
		inside: insideMask(b.g, win.from, win.to), notBefore: b.g.NotBefore(win.bound),
		floor: floor,
	}

	for _, tier := range preferenceTiers {
		if left == 0 {
			break
		}
		b.placeTier(plan, state, sets.primaryMask(b.g, tier), tier, true, false)
		if sets.fallbackAllowed(tier) {
			b.placeTier(plan, state, sets.fallbackMask(b.g, tier), tier, false, true)
		}
	}
	plan.shortfall = left
	return searchResult{
		window:    win,
		placement: Placement{Floor: floor, RequiredSlots: plan.n},
		timeMaps:  trace,
	}
}

// tierState carries the mutable placement cursor across tier passes.
type tierState struct {
	left      *int
	next      *int
	trace     *TimeMaps
	window    grid.Mask
	inside    grid.Mask
	notBefore grid.Mask
	floor     int
}

func insideMask(g *grid.Grid, from, to time.Time) grid.Mask {
	return g.Inside(from, to)
}

// placeTier runs one tier pass: filter eligible runs, choose placement,
// emit blocks and record the trace pass.
func (b *builder) placeTier(plan *taskPlan, state *tierState, mask grid.Mask, tier model.Tier, isPrimary bool, fallbackAllowed bool) {
	if *state.left == 0 {
		return
	}
	before := *state.left
	tierInWindow := mask.And(state.window)
	// Snapshot busy before this pass emits, so placed-early pressure is the
	// delta from earlier tasks (displacement), not this pass's own blocks.
	busyBefore := b.busy.Clone()
	free := tierInWindow.AndNot(busyBefore)
	runs, belowFloorRuns, belowFloorSlots := eligibleRuns(b.g, free, state.floor)
	allocs, comparator := choosePlacementTrace(runs, *state.left, state.floor)
	for _, allocation := range allocs {
		for plan.reserved.Has(*state.next) {
			*state.next++
		}
		first := runs[allocation.run].run.Start
		block := model.Block{
			TaskID: plan.task.ID,
			Index:  *state.next,
			Start:  b.g.Start(first),
			End:    b.g.End(first + allocation.slots - 1),
			Slots:  allocation.slots,
			Kind:   model.BlockFlexible,
		}
		b.addFlexible(block, runs[allocation.run].run)
		plan.placed += allocation.slots
		*state.left -= allocation.slots
		*state.next++
	}
	// Exclusive busy breakdown: pin wins over pinned, pinned over placed,
	// placed over external, so each rejected slot has one reason.
	pinSlots := tierInWindow.And(b.pinMask)
	pinnedSlots := tierInWindow.And(b.pinnedMask).AndNot(pinSlots)
	placedSoFar := busyBefore.AndNot(b.baseBusy)
	placedSlots := tierInWindow.And(placedSoFar).AndNot(pinSlots).AndNot(pinnedSlots)
	externalSlots := tierInWindow.And(b.external).AndNot(pinSlots).AndNot(pinnedSlots).AndNot(tierInWindow.And(placedSoFar))
	beforeBound := mask.AndNot(state.notBefore)
	outsidePlan := mask.And(state.notBefore).AndNot(state.inside)
	pass := TierPass{
		Tier:                string(tier),
		Primary:             isPrimary,
		FallbackAllowed:     fallbackAllowed,
		SlotsBefore:         before,
		SlotsPlaced:         before - *state.left,
		SlotsAfter:          *state.left,
		Floor:               state.floor,
		EligibleTierSlots:   mask.Count(),
		WindowSlots:         tierInWindow.Count(),
		FreeSlots:           free.Count(),
		RunsConsidered:      len(runs),
		RunsBelowFloor:      belowFloorRuns,
		RejectedBusy:        tierInWindow.And(busyBefore).Count(),
		RejectedOutOfWindow: mask.AndNot(state.window).Count(),
		RejectedBelowFloor:  belowFloorSlots,
		RejectedExternal:    externalSlots.Count(),
		RejectedPin:         pinSlots.Count(),
		RejectedPinned:      pinnedSlots.Count(),
		RejectedPlaced:      placedSlots.Count(),
		RejectedBeforeBound: beforeBound.Count(),
		RejectedOutsidePlan: outsidePlan.Count(),
	}
	if len(allocs) > 0 || len(runs) > 0 {
		comparator.Allocations = comparator.Allocations[:0]
		for _, allocation := range allocs {
			comparator.Allocations = append(comparator.Allocations, Allocation{
				RunStart: b.g.Start(runs[allocation.run].run.Start),
				RunSlots: runs[allocation.run].run.Slots,
				Slots:    allocation.slots,
			})
		}
		pass.Comparator = &comparator
	}
	state.trace.Passes = append(state.trace.Passes, pass)
}

// eligibleRuns partitions free runs into placeable runs and below-floor
// rejects, so the minimum-Block floor never leaks into placement.
func eligibleRuns(g *grid.Grid, free grid.Mask, floor int) (runs []placementRun, belowFloorRuns int, belowFloorSlots int) {
	allRuns := free.Runs(g.Len())
	runs = make([]placementRun, 0, len(allRuns))
	for _, gridRun := range allRuns {
		if gridRun.Slots >= floor {
			runs = append(runs, placementRun{run: gridRun, day: model.CivilDateOf(g.Start(gridRun.Start))})
		} else {
			belowFloorRuns++
			belowFloorSlots += gridRun.Slots
		}
	}
	return runs, belowFloorRuns, belowFloorSlots
}

// sorted returns the blocks and explanations in chronological order; ties are
// broken by Task and Block index so the output is identical every run.
func (b *builder) sorted() ([]model.Block, []Explanation) {
	order := make([]int, len(b.blocks))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(x, y int) int {
		blkA, blkB := b.blocks[x], b.blocks[y]
		return cmp.Or(blkA.Start.Compare(blkB.Start), cmp.Compare(blkA.TaskID, blkB.TaskID), cmp.Compare(blkA.Index, blkB.Index))
	})
	blocks := make([]model.Block, len(order))
	expls := make([]Explanation, len(order))
	for i, o := range order {
		blocks[i] = b.blocks[o]
		expls[i] = b.expls[o]
	}
	return blocks, expls
}
