// Package planner turns resolved Tasks into a Schedule.
//
// Plan is a pure function: its only inputs are the ones passed in, it reads no
// files, makes no network calls and never looks at the wall clock, and
// identical inputs always produce identical output (ADR 0006).
package planner

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// Input is everything the planner is allowed to look at. Busy holds the busy
// time already projected onto the same grid the planner will build (nil means
// no busy time); Dispositions must contain only the dispositions that still
// apply, since deciding that is reconciliation's job, not the planner's.
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
type Reason string

const (
	ReasonNoEligibleTime        Reason = "no eligible time in the Scheduling window"
	ReasonPartialFit            Reason = "not enough eligible time to place the whole Task"
	ReasonBlockedByDependency   Reason = "blocked by incomplete dependencies"
	ReasonDependencyCycle       Reason = "dependency cycle: Tasks in the cycle are left unscheduled"
	ReasonUnknownDependency     Reason = "unknown dependency: depends:: references no Task"
	ReasonPinPrecedesDependency Reason = "pin precedes dependency: the Pin stays, the order is violated"
)

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

// Plan computes the Schedule for the given inputs.
func Plan(in Input) (Schedule, error) {
	if err := validateInput(in); err != nil {
		return Schedule{}, err
	}

	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)
	busy, err := buildBusy(in.Busy, g)
	if err != nil {
		return Schedule{}, err
	}

	dispositions, err := indexDispositions(in.Dispositions)
	if err != nil {
		return Schedule{}, err
	}

	plans, err := buildPlans(in.Tasks, dispositions, in.Granularity)
	if err != nil {
		return Schedule{}, err
	}

	b := &builder{g: g, busy: busy}

	// Pins are immovable and exact, so they shape the timeline before any
	// search begins. They are emitted at their authored time, never snapped.
	// A Pin with dependencies stays (Q10 Pin wins); the violation is
	// reported after placement.
	pins := collectPins(plans)
	emitPins(pins, b)
	emitPinned(plans, b, in.Granularity)

	// Order the search: topological over dependencies first (deps before
	// dependents, even across time-bound/flexible), existing comparator as
	// tie-break among unrelated Tasks. Containers never enter the order.
	today := model.CivilDateOf(in.Now.In(in.Location))
	horizonEnd := today.AddDays(in.WindowDays)

	byID, lowerByID := indexPlans(plans)
	candidates := collectCandidates(plans)
	blockedUnknown := findUnknownDeps(candidates, byID, lowerByID)
	order, cycleSet := topoOrder(candidates, byID, lowerByID, blockedUnknown)

	maps := indexTimeMaps(in.TimeMaps)
	ends := initialEnds(pins, plans)

	blockedReason, blockedBy := placeOrdered(b, order, byID, lowerByID, cycleSet, ends, maps, today, horizonEnd, in.Now)

	report := buildReport(candidates, order, pins, byID, lowerByID, cycleSet, blockedUnknown, blockedReason, blockedBy, ends)

	blocks, explanations := b.sorted()
	return Schedule{Blocks: blocks, Report: report, Explanations: explanations}, nil
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

func buildPlans(tasks []model.Task, dispositions map[string][]model.Disposition, granularity time.Duration) ([]*taskPlan, error) {
	plans := make([]*taskPlan, len(tasks))
	for i, t := range tasks {
		if t.Container {
			plans[i] = newTaskPlan(t, nil, granularity)
			plans[i].n = 0
			plans[i].placed = 0
			plans[i].shortfall = 0
			plans[i].stoodDown = 0
			plans[i].pinnedSlots = 0
			continue
		}
		if t.Duration <= 0 {
			return nil, fmt.Errorf("planner: task %s: duration must be positive", t.ID)
		}
		plans[i] = newTaskPlan(t, dispositions[t.ID], granularity)
	}
	return plans, nil
}

func collectPins(plans []*taskPlan) []*taskPlan {
	pins := make([]*taskPlan, 0, len(plans))
	for _, p := range plans {
		if p.task.Pin != nil && !p.task.Container {
			pins = append(pins, p)
		}
	}
	slices.SortFunc(pins, func(a, b *taskPlan) int {
		if !a.task.Pin.Equal(*b.task.Pin) {
			if a.task.Pin.Before(*b.task.Pin) {
				return -1
			}
			return 1
		}
		return compareTasks(a.task, b.task)
	})
	return pins
}

func emitPins(pins []*taskPlan, b *builder) {
	for _, p := range pins {
		start := *p.task.Pin
		b.addExact(model.Block{
			TaskID: p.task.ID,
			Index:  0,
			Start:  start,
			End:    start.Add(p.task.Duration),
			Slots:  p.n,
			Kind:   model.BlockPin,
		})
	}
}

// emitPinned keeps the user-moved spans and marks every slot they overlap
// busy, so nothing else may overlap them.
func emitPinned(plans []*taskPlan, b *builder, granularity time.Duration) {
	for _, p := range plans {
		if p.task.Container {
			continue
		}
		slices.SortFunc(p.pinnedDisps, func(a, b model.Disposition) int {
			if !a.Start.Equal(b.Start) {
				if a.Start.Before(b.Start) {
					return -1
				}
				return 1
			}
			return a.BlockIndex - b.BlockIndex
		})
		for _, d := range p.pinnedDisps {
			b.addExact(model.Block{
				TaskID: p.task.ID,
				Index:  d.BlockIndex,
				Start:  d.Start,
				End:    d.End,
				Slots:  norm.Units(d.End.Sub(d.Start), granularity),
				Kind:   model.BlockPinned,
			})
		}
	}
}

func indexPlans(plans []*taskPlan) (byID, lowerByID map[string]*taskPlan) {
	byID = make(map[string]*taskPlan, len(plans))
	for _, p := range plans {
		if p.task.Container {
			continue
		}
		byID[p.task.ID] = p
	}
	lowerByID = make(map[string]*taskPlan, len(byID))
	for id, p := range byID {
		lowerByID[norm.ID(id)] = p
	}
	return byID, lowerByID
}

// collectCandidates partitions placeable non-Pin Tasks; Pins are already placed.
func collectCandidates(plans []*taskPlan) []*taskPlan {
	var candidates []*taskPlan
	for _, p := range plans {
		if p.task.Container || p.task.Pin != nil || p.required() == 0 {
			continue
		}
		candidates = append(candidates, p)
	}
	return candidates
}

// findUnknownDeps collects dangling (unknown) dependencies, which block immediately.
func findUnknownDeps(candidates []*taskPlan, byID, lowerByID map[string]*taskPlan) map[string][]string {
	blockedUnknown := map[string][]string{}
	for _, p := range candidates {
		for _, dep := range p.task.Depends {
			if _, ok := lookupDep(byID, lowerByID, dep); ok {
				continue
			}
			blockedUnknown[p.task.ID] = append(blockedUnknown[p.task.ID], dep)
		}
	}
	return blockedUnknown
}

// topoOrder runs Kahn over Task-to-Task edges (placeable non-Pin only; Pin
// edges are temporal only since Pins are already placed) with the existing
// comparator as tie-break among unrelated Tasks.
func topoOrder(candidates []*taskPlan, byID, lowerByID map[string]*taskPlan, blockedUnknown map[string][]string) (order []*taskPlan, cycleSet map[string]bool) {
	indeg := make(map[string]int)
	dependents := make(map[string][]string)
	for _, p := range candidates {
		if _, blocked := blockedUnknown[p.task.ID]; blocked {
			continue
		}
		set := map[string]bool{}
		for _, dep := range p.task.Depends {
			depPlan, ok := lookupDep(byID, lowerByID, dep)
			if !ok {
				continue
			}
			if depPlan.task.Pin != nil || depPlan.task.Container {
				continue
			}
			if depPlan.required() == 0 {
				continue // fully stood-down/pinned: temporal only, handled in gating
			}
			if depPlan.task.ID == p.task.ID {
				continue // self-edge handled as cycle below
			}
			if !set[depPlan.task.ID] {
				set[depPlan.task.ID] = true
				indeg[p.task.ID]++
				dependents[depPlan.task.ID] = append(dependents[depPlan.task.ID], p.task.ID)
			}
		}
	}
	// Self-dependencies are cycles.
	for _, p := range candidates {
		for _, dep := range p.task.Depends {
			if norm.ID(dep) == norm.ID(p.task.ID) {
				indeg[p.task.ID]++
			}
		}
	}

	isTimeBound := func(p *taskPlan) bool { return p.task.PlanStart != nil || p.task.PlanEnd != nil }
	lessPlan := func(a, b *taskPlan) bool {
		ab, bb := isTimeBound(a), isTimeBound(b)
		if ab != bb {
			return ab && !bb
		}
		if ab {
			return timeBoundLess(a.task, b.task)
		}
		return orderBefore(a.task, b.task)
	}
	var avail []*taskPlan
	for _, p := range candidates {
		if _, blocked := blockedUnknown[p.task.ID]; blocked {
			continue
		}
		if indeg[p.task.ID] == 0 {
			avail = append(avail, p)
		}
	}
	slices.SortFunc(avail, func(a, b *taskPlan) int {
		if lessPlan(a, b) {
			return -1
		}
		if lessPlan(b, a) {
			return 1
		}
		return 0
	})
	for len(avail) > 0 {
		p := avail[0]
		avail = avail[1:]
		order = append(order, p)
		for _, depID := range dependents[p.task.ID] {
			indeg[depID]--
			if indeg[depID] == 0 {
				if depPlan, ok := byID[depID]; ok {
					if _, blocked := blockedUnknown[depID]; !blocked {
						avail = append(avail, depPlan)
					}
				}
			}
		}
		slices.SortFunc(avail, func(a, b *taskPlan) int {
			if lessPlan(a, b) {
				return -1
			}
			if lessPlan(b, a) {
				return 1
			}
			return 0
		})
	}
	inOrder := map[string]bool{}
	for _, p := range order {
		inOrder[p.task.ID] = true
	}
	// Remaining candidates with indegree>0 (or dangling) are cycle/blocked.
	cycleSet = map[string]bool{}
	for _, p := range candidates {
		if !inOrder[p.task.ID] {
			if _, blocked := blockedUnknown[p.task.ID]; !blocked {
				cycleSet[p.task.ID] = true
			}
		}
	}
	return order, cycleSet
}

// initialEnds tracks ends for dependency gating (Pins + pinned already placed).
func initialEnds(pins, plans []*taskPlan) map[string]time.Time {
	ends := map[string]time.Time{}
	for _, p := range pins {
		ends[p.task.ID] = p.task.Pin.Add(p.task.Duration)
	}
	for _, p := range plans {
		if p.task.Container {
			continue
		}
		if len(p.pinnedDisps) > 0 {
			max := ends[p.task.ID]
			found := false
			for _, d := range p.pinnedDisps {
				if !found || d.End.After(max) {
					max = d.End
					found = true
				}
			}
			if found {
				ends[p.task.ID] = max
			}
		}
	}
	return ends
}

// placeOrdered gates each Task on its dependencies (Q8, Q14) and places the
// survivors. It records ends as the max of pinned + new flexible blocks.
func placeOrdered(b *builder, order []*taskPlan, byID, lowerByID map[string]*taskPlan, cycleSet map[string]bool, ends map[string]time.Time, maps map[string]model.TimeMap, today model.CivilDate, horizonEnd model.CivilDate, now time.Time) (blockedReason map[string]Reason, blockedBy map[string][]string) {
	blockedReason = map[string]Reason{}
	blockedBy = map[string][]string{}

	for _, p := range order {
		beyondHorizon := p.task.PlanStart != nil && !p.task.PlanStart.Before(horizonEnd)
		if beyondHorizon {
			continue // not due yet; left unplaced silently
		}
		// Full-gate: any dep stood-down, shortfall, unplaced, or cycle
		// blocks B entirely (Q8, Q14).
		var blockers []string
		var depBound time.Time
		hasBound := false
		blocked := false
		for _, dep := range p.task.Depends {
			depPlan, ok := lookupDep(byID, lowerByID, dep)
			if !ok {
				blockers = append(blockers, dep)
				blocked = true
				continue
			}
			if depPlan.task.Container {
				continue
			}
			if depPlan.task.Pin != nil {
				if e, ok := ends[depPlan.task.ID]; ok {
					if !hasBound || e.After(depBound) {
						depBound = e
						hasBound = true
					}
				}
				continue
			}
			if cycleSet[depPlan.task.ID] {
				blockers = append(blockers, depPlan.task.ID)
				blocked = true
				continue
			}
			if depPlan.stoodDown > 0 {
				blockers = append(blockers, depPlan.task.ID)
				blocked = true
				continue
			}
			if depPlan.shortfall > 0 {
				blockers = append(blockers, depPlan.task.ID)
				blocked = true
				continue
			}
			e, ok := ends[depPlan.task.ID]
			if !ok {
				// Dep beyond horizon (silently skipped) or otherwise
				// unplaced: B waits.
				blockers = append(blockers, depPlan.task.ID)
				blocked = true
				continue
			}
			if !hasBound || e.After(depBound) {
				depBound = e
				hasBound = true
			}
		}
		if blocked {
			p.shortfall = p.required()
			slices.Sort(blockers)
			blockedReason[p.task.ID] = ReasonBlockedByDependency
			blockedBy[p.task.ID] = blockers
			continue
		}
		bound := now
		if hasBound && depBound.After(bound) {
			bound = depBound
		}
		b.placeWithBound(p, maps, overdue(p.task, today), bound)
		// Record end as max of pinned + new flexible blocks.
		max, ok := ends[p.task.ID]
		for _, blk := range b.blocks {
			if blk.TaskID != p.task.ID {
				continue
			}
			if !ok || blk.End.After(max) {
				max = blk.End
				ok = true
			}
		}
		if ok {
			ends[p.task.ID] = max
		}
	}
	return blockedReason, blockedBy
}

func buildReport(candidates, order, pins []*taskPlan, byID, lowerByID map[string]*taskPlan, cycleSet map[string]bool, blockedUnknown map[string][]string, blockedReason map[string]Reason, blockedBy map[string][]string, ends map[string]time.Time) Report {
	var report Report
	for _, p := range candidates {
		if _, ok := blockedUnknown[p.task.ID]; ok {
			ids := append([]string(nil), blockedUnknown[p.task.ID]...)
			slices.Sort(ids)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    p.task.ID,
				Required:  p.n,
				Placed:    p.placed,
				StoodDown: p.stoodDown,
				Shortfall: p.shortfall + p.required(),
				Reason:    ReasonUnknownDependency,
				BlockedBy: ids,
			})
			continue
		}
		if cycleSet[p.task.ID] {
			// Blocker: the cycle neighbours for context.
			var bl []string
			for _, dep := range p.task.Depends {
				if d, ok := lookupDep(byID, lowerByID, dep); ok {
					bl = append(bl, d.task.ID)
				}
			}
			slices.Sort(bl)
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    p.task.ID,
				Required:  p.n,
				Placed:    p.placed,
				StoodDown: p.stoodDown,
				Shortfall: p.n - p.stoodDown - p.pinnedSlots,
				Reason:    ReasonDependencyCycle,
				BlockedBy: bl,
			})
			continue
		}
	}
	for _, p := range order {
		if r, ok := blockedReason[p.task.ID]; ok {
			report.Tasks = append(report.Tasks, TaskReport{
				TaskID:    p.task.ID,
				Required:  p.n,
				Placed:    p.placed,
				StoodDown: p.stoodDown,
				Shortfall: p.shortfall,
				Reason:    r,
				BlockedBy: append([]string(nil), blockedBy[p.task.ID]...),
			})
			continue
		}
		if p.shortfall <= 0 {
			continue
		}
		reason := ReasonPartialFit
		if p.placed == p.pinnedSlots {
			reason = ReasonNoEligibleTime
		}
		var bl []string
		if len(p.task.Depends) > 0 {
			// Time-bound unmet warning: dependency pressure narrows the
			// window; name the deps so the warning is actionable (Q11).
			reason = ReasonBlockedByDependency
			bl = append(bl, p.task.Depends...)
			slices.Sort(bl)
		}
		report.Tasks = append(report.Tasks, TaskReport{
			TaskID:    p.task.ID,
			Required:  p.n,
			Placed:    p.placed,
			StoodDown: p.stoodDown,
			Shortfall: p.shortfall,
			Reason:    reason,
			BlockedBy: bl,
		})
	}
	// Pin-precedes-dependency violations: Pins stay, loudly reported (Q10).
	for _, p := range pins {
		if len(p.task.Depends) == 0 {
			continue
		}
		start := *p.task.Pin
		var viol []string
		for _, dep := range p.task.Depends {
			depPlan, ok := lookupDep(byID, lowerByID, dep)
			if !ok {
				viol = append(viol, dep)
				continue
			}
			if depPlan.task.Container {
				continue
			}
			if e, ok := ends[depPlan.task.ID]; ok {
				if start.Before(e) {
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
				TaskID:    p.task.ID,
				Required:  p.n,
				Placed:    p.placed,
				StoodDown: p.stoodDown,
				Shortfall: 0,
				Reason:    ReasonPinPrecedesDependency,
				BlockedBy: viol,
			})
		}
	}

	return report
}

// builder accumulates Blocks while blocking their slots on busy.
type builder struct {
	g      *grid.Grid
	busy   grid.Mask
	blocks []model.Block
	expls  []Explanation
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
func (b *builder) placeWithBound(p *taskPlan, maps map[string]model.TimeMap, overdue bool, bound time.Time) {
	from, to := b.g.WindowStart(), b.g.WindowEnd()
	if !overdue {
		if p.task.PlanStart != nil {
			from = p.task.PlanStart.Midnight(b.g.Location())
		}
		if p.task.PlanEnd != nil {
			to = p.task.PlanEnd.AddDays(1).Midnight(b.g.Location())
		}
	}
	window := b.g.Inside(from, to).And(b.g.NotBefore(bound))

	floor := 2
	if p.n == 1 {
		floor = 1
	}

	left := p.required()
	next := 0
	primary, fallback := b.splitMaps(p.task, maps)
	best := bestTierRank(primary)
	placeMask := func(mask grid.Mask) {
		if left == 0 {
			return
		}
		free := mask.And(window).AndNot(b.busy)
		runs := make([]placementRun, 0, 8)
		for _, r := range free.Runs(b.g.Len()) {
			if r.Slots >= floor {
				runs = append(runs, placementRun{run: r, day: model.CivilDateOf(b.g.Start(r.Start))})
			}
		}
		for _, a := range choosePlacement(runs, left, floor) {
			for p.reserved[next] {
				next++
			}
			first := runs[a.run].run.Start
			block := model.Block{
				TaskID: p.task.ID,
				Index:  next,
				Start:  b.g.Start(first),
				End:    b.g.End(first + a.slots - 1),
				Slots:  a.slots,
				Kind:   model.BlockFlexible,
			}
			b.addFlexible(block, runs[a.run].run)
			p.placed += a.slots
			left -= a.slots
			next++
		}
	}
	for _, tier := range []model.Tier{model.TierGreen, model.TierYellow, model.TierRed} {
		if left == 0 {
			break
		}
		placeMask(b.eligibilityFor(primary, tier))
		if fallback != nil && tierRank(tier) >= best {
			placeMask(b.eligibilityFor(fallback, tier))
		}
	}
	p.shortfall = left
}

// splitMaps partitions the Time Maps a Task may use into its primary set and
// its default fallback. A Task with no explicit time-map:: uses the default
// maps with no fallback. A Task naming its own maps keeps those as primary
// and spills into the default maps; unknown-only names resolve to nothing
// and stay strict so the unknown-time-map issue is not masked.
func (b *builder) splitMaps(t model.Task, maps map[string]model.TimeMap) (primary, fallback []model.TimeMap) {
	var defaults []model.TimeMap
	for _, tm := range maps {
		if tm.Default {
			defaults = append(defaults, tm)
		}
	}
	if len(t.TimeMaps) == 0 {
		return defaults, nil
	}
	primarySlugs := map[string]bool{}
	for _, slug := range t.TimeMaps {
		if tm, ok := maps[slug]; ok {
			primary = append(primary, tm)
			primarySlugs[tm.Slug] = true
		}
	}
	if len(primary) == 0 {
		return nil, nil
	}
	for _, tm := range defaults {
		if !primarySlugs[tm.Slug] {
			fallback = append(fallback, tm)
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
	for _, tm := range maps {
		filtered := model.TimeMap{Windows: windowsOfTier(tm.Windows, tier)}
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
// no windows at all ranks worst, so an empty project map spills everything
// into the default map instead of shortfalling.
func bestTierRank(maps []model.TimeMap) int {
	best := 2
	found := false
	for _, tm := range maps {
		for _, w := range tm.Windows {
			if r := tierRank(windowTier(w)); !found || r < best {
				best = r
				found = true
			}
		}
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
		a, b := b.blocks[x], b.blocks[y]
		if !a.Start.Equal(b.Start) {
			if a.Start.Before(b.Start) {
				return -1
			}
			return 1
		}
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return a.Index - b.Index
	})
	blocks := make([]model.Block, len(order))
	expls := make([]Explanation, len(order))
	for i, o := range order {
		blocks[i] = b.blocks[o]
		expls[i] = b.expls[o]
	}
	return blocks, expls
}

// taskPlan accumulates one Task's placement state.
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
}

func newTaskPlan(t model.Task, dispositions []model.Disposition, granularity time.Duration) *taskPlan {
	p := &taskPlan{task: t, n: norm.Units(t.Duration, granularity), reserved: make(map[int]bool)}

	stoodDown := 0
	pinned := 0
	for _, d := range dispositions {
		p.reserved[d.BlockIndex] = true
		switch d.Kind {
		case model.DispositionPinned:
			p.pinnedDisps = append(p.pinnedDisps, d)
			pinned += norm.Units(d.End.Sub(d.Start), granularity)
		case model.DispositionStoodDown:
			p.stoodDownDisps = append(p.stoodDownDisps, d)
			stoodDown += d.Slots
		}
	}

	if t.Pin != nil {
		// A Pin overrides Plan, Priority and any recorded dispositions.
		p.pinnedDisps = nil
		p.stoodDownDisps = nil
		p.reserved = map[int]bool{}
		p.pinnedSlots = p.n
		p.placed = p.n
		return p
	}

	p.stoodDown = min(stoodDown, p.n)
	p.pinnedSlots = min(pinned, p.n-p.stoodDown)
	p.placed = p.pinnedSlots
	return p
}

// required returns the units still to place flexibly.
func (p *taskPlan) required() int { return p.n - p.stoodDown - p.pinnedSlots }

func indexDispositions(dispositions []model.Disposition) (map[string][]model.Disposition, error) {
	out := make(map[string][]model.Disposition)
	type key struct {
		task  string
		block int
	}
	seen := make(map[key]bool)
	for _, d := range dispositions {
		switch d.Kind {
		case model.DispositionPinned:
			if !d.Start.Before(d.End) {
				return nil, fmt.Errorf("planner: task %s block %d: pinned disposition has an empty span", d.TaskID, d.BlockIndex)
			}
		case model.DispositionStoodDown:
			if d.Slots < 0 {
				return nil, fmt.Errorf("planner: task %s block %d: stood-down disposition has negative size", d.TaskID, d.BlockIndex)
			}
		default:
			return nil, fmt.Errorf("planner: task %s block %d: unknown disposition kind %q", d.TaskID, d.BlockIndex, d.Kind)
		}
		if d.BlockIndex < 0 {
			return nil, fmt.Errorf("planner: task %s: negative block index %d", d.TaskID, d.BlockIndex)
		}
		k := key{task: d.TaskID, block: d.BlockIndex}
		if seen[k] {
			return nil, fmt.Errorf("planner: task %s: more than one disposition for block %d", d.TaskID, d.BlockIndex)
		}
		seen[k] = true
		out[d.TaskID] = append(out[d.TaskID], d)
	}
	return out, nil
}

func indexTimeMaps(maps []model.TimeMap) map[string]model.TimeMap {
	out := make(map[string]model.TimeMap, len(maps))
	for _, tm := range maps {
		out[tm.Slug] = tm
	}
	return out
}

// lookupDep resolves one depends:: entry to its plan: exact ID first, then
// case-insensitive match. ok is false for dangling refs, which block.
func lookupDep(byID, lowerByID map[string]*taskPlan, dep string) (depPlan *taskPlan, ok bool) {
	if d, found := byID[dep]; found {
		return d, true
	}
	if d, found := lowerByID[norm.ID(dep)]; found {
		return d, true
	}
	return nil, false
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
	if ra, rb := a.Priority.Rank(), b.Priority.Rank(); ra != rb {
		return ra - rb
	}
	if a.OutlineOrder != b.OutlineOrder {
		return a.OutlineOrder - b.OutlineOrder
	}
	return strings.Compare(a.ID, b.ID)
}
