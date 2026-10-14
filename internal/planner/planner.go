// Package planner turns resolved Tasks into a Schedule.
//
// Plan is a pure function: its only inputs are the ones passed in, it reads no
// files, makes no network calls and never looks at the wall clock, and
// identical inputs always produce identical output (ADR 0006).
package planner

import (
	"fmt"
	"sort"
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
}

// Reason explains a shortfall.
type Reason string

const (
	ReasonNoEligibleTime Reason = "no eligible time in the Scheduling window"
	ReasonPartialFit     Reason = "not enough eligible time to place the whole Task"
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
		if t.Duration <= 0 {
			return Schedule{}, fmt.Errorf("planner: task %s: duration must be positive", t.ID)
		}
		plans[i] = newTaskPlan(t, dispositions[t.ID], in.Granularity)
	}

	b := &builder{g: g, busy: busy}

	// Pins are immovable and exact, so they shape the timeline before any
	// search begins. They are emitted at their authored time, never snapped.
	pins := make([]*taskPlan, 0, len(plans))
	for _, p := range plans {
		if p.task.Pin != nil {
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

	// Order the search: time-bound Tasks first (plan-end ascending, then
	// Priority, then outline order), flexible Tasks last (Priority, then
	// outline order). Overdue Tasks stay time-bound and sort first.
	today := model.CivilDateOf(in.Now.In(in.Location))
	horizonEnd := today.AddDays(in.WindowDays)

	var timeBound, flexible []*taskPlan
	for _, p := range plans {
		if p.required() == 0 {
			continue
		}
		if p.task.PlanStart != nil || p.task.PlanEnd != nil {
			timeBound = append(timeBound, p)
		} else {
			flexible = append(flexible, p)
		}
	}
	sort.Slice(timeBound, func(i, j int) bool { return timeBoundLess(timeBound[i].task, timeBound[j].task) })
	sort.Slice(flexible, func(i, j int) bool { return orderBefore(flexible[i].task, flexible[j].task) })

	order := append(timeBound, flexible...)
	maps := indexTimeMaps(in.TimeMaps)
	for _, p := range order {
		beyondHorizon := p.task.PlanStart != nil && !p.task.PlanStart.Before(horizonEnd)
		if beyondHorizon {
			continue // not due yet; left unplaced silently
		}
		b.place(p, maps, overdue(p.task, today), in.Now)
	}

	var report Report
	for _, p := range order {
		if p.shortfall <= 0 {
			continue
		}
		reason := ReasonPartialFit
		if p.placed == 0 {
			reason = ReasonNoEligibleTime
		}
		report.Tasks = append(report.Tasks, TaskReport{
			TaskID:    p.task.ID,
			Required:  p.n,
			Placed:    p.placed,
			StoodDown: p.stoodDown,
			Shortfall: p.shortfall,
			Reason:    reason,
		})
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
func (b *builder) place(p *taskPlan, maps map[string]model.TimeMap, overdue bool, now time.Time) {
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
	free = free.And(b.g.Inside(from, to)).And(b.g.NotBefore(now)).AndNot(b.busy)

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
