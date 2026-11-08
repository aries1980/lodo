package planner

import (
	"slices"
	"strings"
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

	traceNow        time.Time
	traceToday      model.CivilDate
	traceHorizonEnd model.CivilDate
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

// place searches the Task's free slots and emits its flexible Blocks.
// Bound is the dependency-aware lower bound: the Task may not start before
// bound (max of now and its dependencies' ends).
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
func (b *builder) placeWithBound(plan *taskPlan, maps map[string]model.TimeMap, overdue bool, bound time.Time) {
	from, to := b.g.WindowStart(), b.g.WindowEnd()
	if !overdue {
		if plan.task.PlanStart != nil {
			from = plan.task.PlanStart.Midnight(b.g.Location())
		}
		if plan.task.PlanEnd != nil {
			to = plan.task.PlanEnd.AddDays(1).Midnight(b.g.Location())
		}
	}
	window := b.g.Inside(from, to).And(b.g.NotBefore(bound))

	floor := 2
	if plan.n == 1 {
		floor = 1
	}

	plan.decision.Window.Overdue = overdue
	plan.decision.Window.From = from
	plan.decision.Window.To = to
	plan.decision.Window.Bound = bound
	if b.traceHorizonEnd.Year != 0 {
		plan.decision.Window.HorizonEnd = b.traceHorizonEnd.String()
	}
	plan.decision.Placement = &Placement{Floor: floor}

	left := plan.required()
	next := 0
	primary, fallback := b.splitMaps(plan.task, maps)
	best := bestTierRank(primary)
	trace := TimeMaps{
		Requested:     append([]string(nil), plan.task.TimeMaps...),
		Primary:       mapSlugs(primary),
		Fallback:      mapSlugs(fallback),
		BestTier:      string(tierByRank(best)),
		NoUpwardSpill: len(plan.task.TimeMaps) > 0 && len(primary) > 0 && fallback != nil,
	}
	state := &tierState{left: &left, next: &next, trace: &trace, window: window, floor: floor}

	for _, tier := range []model.Tier{model.TierGreen, model.TierYellow, model.TierRed} {
		if left == 0 {
			break
		}
		b.placeTier(plan, state, b.eligibilityFor(primary, tier), tier, true, false)
		if fallback != nil && tierRank(tier) >= best {
			b.placeTier(plan, state, b.eligibilityFor(fallback, tier), tier, false, true)
		}
	}
	plan.decision.TimeMaps = trace
	plan.shortfall = left
}

// tierState carries the mutable placement cursor across tier passes.
type tierState struct {
	left   *int
	next   *int
	trace  *TimeMaps
	window grid.Mask
	floor  int
}

// placeTier runs one tier pass: filter eligible runs, choose placement,
// emit blocks and record the trace pass.
func (b *builder) placeTier(plan *taskPlan, state *tierState, mask grid.Mask, tier model.Tier, isPrimary bool, fallbackAllowed bool) {
	if *state.left == 0 {
		return
	}
	before := *state.left
	tierInWindow := mask.And(state.window)
	free := tierInWindow.AndNot(b.busy)
	runs, belowFloorRuns, belowFloorSlots := eligibleRuns(b.g, free, state.floor)
	allocs, comparator := choosePlacementTrace(runs, *state.left, state.floor)
	// Fill run starts for allocations (placement package doesn't have grid).
	for i, allocation := range allocs {
		if allocation.run >= 0 && allocation.run < len(runs) {
			comparator.Allocations[i].RunStart = b.g.Start(runs[allocation.run].run.Start)
		}
	}
	for _, allocation := range allocs {
		for plan.reserved[*state.next] {
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
		RejectedBusy:        tierInWindow.And(b.busy).Count(),
		RejectedOutOfWindow: mask.AndNot(state.window).Count(),
		RejectedBelowFloor:  belowFloorSlots,
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

func mapSlugs(maps []model.TimeMap) []string {
	if len(maps) == 0 {
		return nil
	}
	out := make([]string, 0, len(maps))
	for _, timeMap := range maps {
		out = append(out, timeMap.Slug)
	}
	return out
}

func tierByRank(rank int) model.Tier {
	switch rank {
	case 1:
		return model.TierYellow
	case 2:
		return model.TierRed
	default:
		return model.TierGreen
	}
}

// splitMaps partitions the Time Maps a Task may use into its primary set and
// its default fallback. A Task with no explicit time-map:: uses the default
// maps with no fallback. A Task naming its own maps keeps those as primary
// and spills into the default maps; unknown-only names resolve to nothing
// and stay strict so the unknown-time-map issue is not masked.
func (b *builder) splitMaps(t model.Task, maps map[string]model.TimeMap) (primary, fallback []model.TimeMap) {
	var defaults []model.TimeMap
	for _, timeMap := range maps {
		if timeMap.Default {
			defaults = append(defaults, timeMap)
		}
	}
	if len(t.TimeMaps) == 0 {
		return defaults, nil
	}
	primarySlugs := map[string]bool{}
	for _, slug := range t.TimeMaps {
		if timeMap, ok := maps[slug]; ok {
			primary = append(primary, timeMap)
			primarySlugs[timeMap.Slug] = true
		}
	}
	if len(primary) == 0 {
		return nil, nil
	}
	for _, timeMap := range defaults {
		if !primarySlugs[timeMap.Slug] {
			fallback = append(fallback, timeMap)
		}
	}
	if len(fallback) == 0 {
		fallback = nil
	}
	return primary, fallback
}

// eligibilityFor returns the slots of one preference tier in the given map
// set. A slot covered by two tiers takes the better one: yellow excludes
// green, red excludes both.
func (b *builder) eligibilityFor(selected []model.TimeMap, tier model.Tier) grid.Mask {
	green := tierMask(b.g, selected, model.TierGreen)
	switch tier {
	case model.TierYellow:
		return tierMask(b.g, selected, model.TierYellow).AndNot(green)
	case model.TierRed:
		return tierMask(b.g, selected, model.TierRed).AndNot(green).AndNot(tierMask(b.g, selected, model.TierYellow))
	default:
		return green
	}
}

// tierMask is the union of windows of one tier. An empty or unknown tier
// word counts as green, matching the parser's default.
func tierMask(g *grid.Grid, maps []model.TimeMap, tier model.Tier) grid.Mask {
	m := grid.NewMask(g.Len())
	for _, timeMap := range maps {
		filtered := model.TimeMap{Windows: windowsOfTier(timeMap.Windows, tier)}
		if len(filtered.Windows) == 0 {
			continue
		}
		m.OrInto(g.TimeMapMask(filtered))
	}
	return m
}

func windowsOfTier(windows []model.Window, tier model.Tier) []model.Window {
	var out []model.Window
	for _, w := range windows {
		if windowTier(w) == tier {
			out = append(out, w)
		}
	}
	return out
}

func windowTier(w model.Window) model.Tier {
	switch w.Tier {
	case model.TierYellow, model.TierRed:
		return w.Tier
	default:
		return model.TierGreen
	}
}

// tierRank orders tiers by preference: green first, then yellow, then red.
func tierRank(t model.Tier) int {
	switch t {
	case model.TierYellow:
		return 1
	case model.TierRed:
		return 2
	default:
		return 0
	}
}

// bestTierRank is the best (lowest-rank) tier in the given maps. A set with
// no windows at all ranks green, so an empty project map tries the default
// map from its green tier instead of shortfalling.
func bestTierRank(maps []model.TimeMap) int {
	best := 2
	found := false
	for _, timeMap := range maps {
		for _, w := range timeMap.Windows {
			if rank := tierRank(windowTier(w)); !found || rank < best {
				best = rank
				found = true
			}
		}
	}
	if !found {
		return 0 // empty set: try the fallback from its best tier
	}
	return best
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
		if !blkA.Start.Equal(blkB.Start) {
			if blkA.Start.Before(blkB.Start) {
				return -1
			}
			return 1
		}
		if blkA.TaskID != blkB.TaskID {
			return strings.Compare(blkA.TaskID, blkB.TaskID)
		}
		return blkA.Index - blkB.Index
	})
	blocks := make([]model.Block, len(order))
	expls := make([]Explanation, len(order))
	for i, o := range order {
		blocks[i] = b.blocks[o]
		expls[i] = b.expls[o]
	}
	return blocks, expls
}
