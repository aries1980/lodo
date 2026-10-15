// Package planner turns resolved Tasks into a Schedule.
//
// Plan is a pure function: its only inputs are the ones passed in, it reads no
// files, makes no network calls and never looks at the wall clock, and
// identical inputs always produce identical output (ADR 0006).
package planner

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
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
	if in.Location == nil {
		return Schedule{}, fmt.Errorf("planner: location is required")
	}
	if in.Granularity <= 0 || in.Granularity > 24*time.Hour {
		return Schedule{}, fmt.Errorf("planner: granularity must be within (0h, 24h], got %v", in.Granularity)
	}
	if in.WindowDays <= 0 {
		return Schedule{}, fmt.Errorf("planner: window days must be positive, got %d", in.WindowDays)
	}

	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)

	busy := grid.NewMask(g.Len())
	if len(in.Busy) > 0 {
		if len(in.Busy) != g.MaskWords() {
			return Schedule{}, fmt.Errorf("planner: busy mask has %d words, want %d", len(in.Busy), g.MaskWords())
		}
		busy = in.Busy.Clone()
	}

	dispositions, err := indexDispositions(in.Dispositions)
	if err != nil {
		return Schedule{}, err
	}

	plans := make([]*taskPlan, len(in.Tasks))
	for i, t := range in.Tasks {
		if t.Container {
			plans[i] = newTaskPlan(t, nil, in.Granularity)
			plans[i].n = 0
			plans[i].placed = 0
			plans[i].shortfall = 0
			plans[i].stoodDown = 0
			plans[i].pinnedSlots = 0
			continue
		}
		if t.Duration <= 0 {
			return Schedule{}, fmt.Errorf("planner: task %s: duration must be positive", t.ID)
		}
		plans[i] = newTaskPlan(t, dispositions[t.ID], in.Granularity)
	}

	b := &builder{g: g, busy: busy}

	// Pins are immovable and exact, so they shape the timeline before any
	// search begins. They are emitted at their authored time, never snapped.
	// A Pin with dependencies stays (Q10 Pin wins); the violation is
	// reported after placement.
	pins := make([]*taskPlan, 0, len(plans))
	for _, p := range plans {
		if p.task.Pin != nil && !p.task.Container {
			pins = append(pins, p)
		}
	}
	sort.Slice(pins, func(i, j int) bool {
		a, b := pins[i].task, pins[j].task
		if !a.Pin.Equal(*b.Pin) {
			return a.Pin.Before(*b.Pin)
		}
		return orderBefore(a, b)
	})
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

	// Pinned Blocks keep the span the user moved them to and mark every slot
	// they overlap busy, so nothing else may overlap them.
	for _, p := range plans {
		if p.task.Container {
			continue
		}
		sort.Slice(p.pinnedDisps, func(i, j int) bool {
			a, b := p.pinnedDisps[i], p.pinnedDisps[j]
			if !a.Start.Equal(b.Start) {
				return a.Start.Before(b.Start)
			}
			return a.BlockIndex < b.BlockIndex
		})
		for _, d := range p.pinnedDisps {
			b.addExact(model.Block{
				TaskID: p.task.ID,
				Index:  d.BlockIndex,
				Start:  d.Start,
				End:    d.End,
				Slots:  units(d.End.Sub(d.Start), in.Granularity),
				Kind:   model.BlockPinned,
			})
		}
	}

	// Order the search: topological over dependencies first (deps before
	// dependents, even across time-bound/flexible), existing comparator as
	// tie-break among unrelated Tasks. Containers never enter the order.
	today := model.CivilDateOf(in.Now.In(in.Location))
	horizonEnd := today.AddDays(in.WindowDays)

	byID := make(map[string]*taskPlan, len(plans))
	for _, p := range plans {
		if p.task.Container {
			continue
		}
		byID[p.task.ID] = p
	}
	lowerByID := make(map[string]*taskPlan, len(byID))
	for id, p := range byID {
		lowerByID[strings.ToLower(strings.TrimSpace(id))] = p
	}

	// Partition placeable non-Pin Tasks; Pins are already placed.
	var candidates []*taskPlan
	pinSet := map[string]bool{}
	for _, p := range pins {
		pinSet[p.task.ID] = true
	}
	for _, p := range plans {
		if p.task.Container || p.task.Pin != nil || p.required() == 0 {
			continue
		}
		candidates = append(candidates, p)
	}

	// Dangling (unknown) dependencies block immediately.
	blockedUnknown := map[string][]string{}
	for _, p := range candidates {
		for _, dep := range p.task.Depends {
			if _, ok := byID[dep]; ok {
				continue
			}
			if _, ok := lowerByID[strings.ToLower(strings.TrimSpace(dep))]; ok {
				continue
			}
			blockedUnknown[p.task.ID] = append(blockedUnknown[p.task.ID], dep)
		}
	}

	// Kahn over Task-to-Task edges (placeable non-Pin only; Pin edges are
	// temporal only since Pins are already placed).
	indeg := make(map[string]int)
	dependents := make(map[string][]string)
	for _, p := range candidates {
		if _, blocked := blockedUnknown[p.task.ID]; blocked {
			continue
		}
		set := map[string]bool{}
		for _, dep := range p.task.Depends {
			var depPlan *taskPlan
			if d, ok := byID[dep]; ok {
				depPlan = d
			} else if d, ok := lowerByID[strings.ToLower(strings.TrimSpace(dep))]; ok {
				depPlan = d
			} else {
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
			if strings.EqualFold(strings.TrimSpace(dep), strings.TrimSpace(p.task.ID)) {
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
	sort.Slice(avail, func(i, j int) bool { return lessPlan(avail[i], avail[j]) })
	var order []*taskPlan
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
		sort.Slice(avail, func(i, j int) bool { return lessPlan(avail[i], avail[j]) })
	}
	inOrder := map[string]bool{}
	for _, p := range order {
		inOrder[p.task.ID] = true
	}
	// Remaining candidates with indegree>0 (or dangling) are cycle/blocked.
	cycleSet := map[string]bool{}
	for _, p := range candidates {
		if !inOrder[p.task.ID] {
			if _, blocked := blockedUnknown[p.task.ID]; !blocked {
				cycleSet[p.task.ID] = true
			}
		}
	}

	maps := indexTimeMaps(in.TimeMaps)
	// Track ends for dependency gating (Pins + pinned already placed).
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

	// Helpers for gating.
	blockedReason := map[string]Reason{}
	blockedBy := map[string][]string{}
	taskEnd := func(id string) (time.Time, bool) {
		e, ok := ends[id]
		return e, ok
	}
	_ = taskEnd

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
			var depPlan *taskPlan
			if d, ok := byID[dep]; ok {
				depPlan = d
			} else if d, ok := lowerByID[strings.ToLower(strings.TrimSpace(dep))]; ok {
				depPlan = d
			} else {
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
			sort.Strings(blockers)
			blockedReason[p.task.ID] = ReasonBlockedByDependency
			blockedBy[p.task.ID] = blockers
			continue
		}
		bound := in.Now
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

	var report Report
	for _, p := range candidates {
		if _, ok := blockedUnknown[p.task.ID]; ok {
			ids := append([]string(nil), blockedUnknown[p.task.ID]...)
			sort.Strings(ids)
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
				if _, ok := byID[dep]; ok {
					bl = append(bl, dep)
				} else if d, ok := lowerByID[strings.ToLower(strings.TrimSpace(dep))]; ok {
					bl = append(bl, d.task.ID)
				}
			}
			sort.Strings(bl)
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
			sort.Strings(bl)
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
			var depPlan *taskPlan
			if d, ok := byID[dep]; ok {
				depPlan = d
			} else if d, ok := lowerByID[strings.ToLower(strings.TrimSpace(dep))]; ok {
				depPlan = d
			} else {
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
			sort.Strings(viol)
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

	blocks, explanations := b.sorted()
	return Schedule{Blocks: blocks, Report: report, Explanations: explanations}, nil
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
func (b *builder) placeWithBound(p *taskPlan, maps map[string]model.TimeMap, overdue bool, bound time.Time) {
	free := b.eligibility(p.task, maps)

	from, to := b.g.WindowStart(), b.g.WindowEnd()
	if !overdue {
		if p.task.PlanStart != nil {
			from = p.task.PlanStart.Midnight(b.g.Location())
		}
		if p.task.PlanEnd != nil {
			to = p.task.PlanEnd.AddDays(1).Midnight(b.g.Location())
		}
	}
	free = free.And(b.g.Inside(from, to)).And(b.g.NotBefore(bound)).AndNot(b.busy)

	floor := 2
	if p.n == 1 {
		floor = 1
	}

	runs := make([]placementRun, 0, 8)
	for _, r := range free.Runs(b.g.Len()) {
		if r.Slots >= floor {
			runs = append(runs, placementRun{run: r, day: model.CivilDateOf(b.g.Start(r.Start))})
		}
	}

	next := 0
	for _, a := range choosePlacement(runs, p.required(), floor) {
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
		next++
	}
	p.shortfall = p.required() - (p.placed - p.pinnedSlots)
}

// eligibility returns the union of the Task's Time Maps, or of the default
// maps when the Task names none.
func (b *builder) eligibility(t model.Task, maps map[string]model.TimeMap) grid.Mask {
	m := grid.NewMask(b.g.Len())
	if len(t.TimeMaps) > 0 {
		for _, slug := range t.TimeMaps {
			if tm, ok := maps[slug]; ok {
				m.OrInto(b.g.TimeMapMask(tm))
			}
		}
		return m
	}
	for _, tm := range maps {
		if tm.Default {
			m.OrInto(b.g.TimeMapMask(tm))
		}
	}
	return m
}

// sorted returns the blocks and explanations in chronological order; ties are
// broken by Task and Block index so the output is identical every run.
func (b *builder) sorted() ([]model.Block, []Explanation) {
	order := make([]int, len(b.blocks))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := b.blocks[order[i]], b.blocks[order[j]]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		return a.Index < b.Index
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
	p := &taskPlan{task: t, n: units(t.Duration, granularity), reserved: make(map[int]bool)}

	stoodDown := 0
	pinned := 0
	for _, d := range dispositions {
		p.reserved[d.BlockIndex] = true
		switch d.Kind {
		case model.DispositionPinned:
			p.pinnedDisps = append(p.pinnedDisps, d)
			pinned += units(d.End.Sub(d.Start), granularity)
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

func units(d, granularity time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + granularity - 1) / granularity)
}

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
	if ra, rb := a.Priority.Rank(), b.Priority.Rank(); ra != rb {
		return ra < rb
	}
	if a.OutlineOrder != b.OutlineOrder {
		return a.OutlineOrder < b.OutlineOrder
	}
	return a.ID < b.ID
}
