package planner

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

// fixtureInput builds a week with overlapping Time Maps, busy time, a Pin, a
// Pinned Block, a stood-down Block and an overdue Task, so the properties are
// exercised against a Schedule with all the moving parts at once.
func fixtureInput(t *testing.T) Input {
	t.Helper()

	deep := model.TimeMap{Slug: "deep", Windows: []model.Window{
		{Days: weekdays(), StartMinute: 9 * 60, EndMinute: 12 * 60, Tier: model.TierGreen},
	}}
	weekend := model.TimeMap{Slug: "weekend", Default: true, Windows: []model.Window{
		{Days: []time.Weekday{time.Saturday, time.Sunday}, StartMinute: 10 * 60, EndMinute: 14 * 60, Tier: model.TierGreen},
	}}

	t1 := localTask("t1", duration(120))
	t1.Priority = model.PriorityA

	t2 := localTask("t2", duration(90))
	t2.Priority = model.PriorityC
	t2.OutlineOrder = 1

	t3 := localTask("t3", duration(240))
	end12 := civil(12)
	t3.PlanEnd = &end12
	t3.OutlineOrder = 2

	t4 := model.Task{ID: "t4", Text: "t4", Marker: model.MarkerTODO, Duration: duration(60), OutlineOrder: 3}

	t5 := localTask("t5", duration(120))
	t5.OutlineOrder = 4

	t6 := localTask("t6", duration(90))
	t6.OutlineOrder = 5

	t7 := localTask("t7", duration(45))
	pin := at(11, 11, 30)
	t7.Pin = &pin
	t7.OutlineOrder = 6

	t8 := localTask("t8", duration(60))
	end8 := civil(8)
	t8.PlanEnd = &end8
	t8.OutlineOrder = 7

	t9 := localTask("t9", duration(60))
	start16 := civil(16)
	t9.PlanStart = &start16
	t9.OutlineOrder = 8

	in := Input{
		Tasks:    []model.Task{t1, t2, t3, t4, t5, t6, t7, t8, t9},
		TimeMaps: []model.TimeMap{deep, weekend},
		Dispositions: []model.Disposition{
			{TaskID: "t5", BlockIndex: 1, Kind: model.DispositionPinned, Start: at(10, 10, 0), End: at(10, 10, 30)},
			{TaskID: "t6", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: 2, Snapshot: t6.Snapshot()},
		},
		Now:         at(9, 0, 0),
		Granularity: 15 * time.Minute,
		WindowDays:  7,
		Location:    london,
	}

	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)
	in.Busy = g.Overlapping(at(9, 10, 0), at(9, 11, 0)) // Monday morning meeting
	in.Busy.OrInto(g.Overlapping(at(11, 9, 0), at(11, 9, 30)))
	return in
}

func TestScheduleProperties(t *testing.T) {
	in := fixtureInput(t)

	first, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Determinism: identical inputs, identical output.
	second, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if diff := cmp.Diff(first, second); diff != "" {
		t.Fatalf("Plan is not deterministic (-first +second):\n%s", diff)
	}

	// The Task slice order must not matter.
	reversed := in
	reversed.Tasks = slices.Clone(in.Tasks)
	slices.Reverse(reversed.Tasks)
	third, err := Plan(reversed)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if diff := cmp.Diff(first, third); diff != "" {
		t.Fatalf("input order changed the Schedule (-first +reversed):\n%s", diff)
	}

	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)

	busy := grid.NewMask(g.Len())
	busy.OrInto(in.Busy)
	pinMask := grid.NewMask(g.Len())
	pinnedMask := grid.NewMask(g.Len())
	for _, task := range in.Tasks {
		if task.Pin != nil {
			pinMask.OrInto(g.Overlapping(*task.Pin, task.Pin.Add(task.Duration)))
		}
	}
	for _, d := range in.Dispositions {
		if d.Kind == model.DispositionPinned {
			pinnedMask.OrInto(g.Overlapping(d.Start, d.End))
		}
	}
	occupied := busy.Or(pinMask).Or(pinnedMask)

	byTask := make(map[string][]model.Block)
	for _, b := range first.Blocks {
		byTask[b.TaskID] = append(byTask[b.TaskID], b)
	}
	reported := make(map[string]TaskReport)
	for _, r := range first.Report.Tasks {
		reported[r.TaskID] = r
	}

	today := model.CivilDateOf(in.Now.In(in.Location))
	horizonEnd := today.AddDays(in.WindowDays)
	claimed := grid.NewMask(g.Len())

	for _, task := range in.Tasks {
		n := slotCount(task.Duration, in.Granularity)
		blocks := byTask[task.ID]

		stoodDown := 0
		if task.Pin == nil {
			for _, d := range in.Dispositions {
				if d.TaskID == task.ID && d.Kind == model.DispositionStoodDown {
					stoodDown += d.Slots
				}
			}
		}

		placed := 0
		for _, b := range blocks {
			placed += b.Slots
		}

		// Tasks beyond the horizon are left unplaced silently; they are not
		// part of the arithmetic.
		if task.PlanStart != nil && !task.PlanStart.Before(horizonEnd) {
			if len(blocks) != 0 {
				t.Errorf("task %s is beyond the horizon but has %d Blocks", task.ID, len(blocks))
			}
			continue
		}

		// Shortfall arithmetic: placed + shortfall + stoodDown = n.
		if r := reported[task.ID]; placed+r.Shortfall+stoodDown != n {
			t.Errorf("task %s: placed(%d) + shortfall(%d) + stoodDown(%d) != n(%d)",
				task.ID, placed, r.Shortfall, stoodDown, n)
		}

		overdue := task.PlanEnd != nil && task.PlanEnd.Before(today)
		from, to := g.WindowStart(), g.WindowEnd()
		if !overdue {
			if task.PlanStart != nil {
				from = task.PlanStart.Midnight(in.Location)
			}
			if task.PlanEnd != nil {
				to = task.PlanEnd.AddDays(1).Midnight(in.Location)
			}
		}
		planMask := g.Inside(from, to)
		eligibility := eligibilityMask(g, task, in.TimeMaps)

		seen := make(map[int]bool)
		for _, b := range blocks {
			if seen[b.Index] {
				t.Errorf("task %s: Block index %d used twice", task.ID, b.Index)
			}
			seen[b.Index] = true

			if b.Kind != model.BlockFlexible {
				continue
			}
			start, end := slotRange(t, g, b)
			if n > 1 && b.Slots < 2 {
				t.Errorf("task %s: flexible Block of %d slots is below the floor", task.ID, b.Slots)
			}
			for i := start; i < end; i++ {
				switch {
				case occupied.Has(i):
					t.Errorf("task %s: flexible Block overlaps busy time or a pin", task.ID)
				case !eligibility.Has(i):
					t.Errorf("task %s: flexible Block outside its Time Maps", task.ID)
				case !planMask.Has(i):
					t.Errorf("task %s: flexible Block outside its Plan window", task.ID)
				case g.Start(i).Before(in.Now):
					t.Errorf("task %s: flexible Block in the past", task.ID)
				case claimed.Has(i):
					t.Errorf("task %s: flexible Block overlaps another flexible Block", task.ID)
				}
				claimed.Set(i)
			}
		}
	}

	// The Pin and the Pinned Block are emitted at their exact spans, never
	// snapped, and the stood-down index stays reserved.
	wantPin := []model.Block{{
		TaskID: "t7", Index: 0, Start: at(11, 11, 30), End: at(11, 12, 15), Slots: 3, Kind: model.BlockPin,
	}}
	if diff := cmp.Diff(wantPin, byTask["t7"]); diff != "" {
		t.Fatalf("Pin mismatch (-want +got):\n%s", diff)
	}

	var pinnedBlocks []model.Block
	for _, b := range byTask["t5"] {
		if b.Kind == model.BlockPinned {
			pinnedBlocks = append(pinnedBlocks, b)
		}
	}
	wantPinned := []model.Block{{
		TaskID: "t5", Index: 1, Start: at(10, 10, 0), End: at(10, 10, 30), Slots: 2, Kind: model.BlockPinned,
	}}
	if diff := cmp.Diff(wantPinned, pinnedBlocks); diff != "" {
		t.Fatalf("Pinned Block mismatch (-want +got):\n%s", diff)
	}

	for _, b := range byTask["t6"] {
		if b.Index == 0 {
			t.Fatalf("stood-down Block index must stay reserved, got %+v", b)
		}
	}

	if len(byTask["t9"]) != 0 {
		t.Fatalf("a Task beyond the horizon must stay unplaced, got %+v", byTask["t9"])
	}
}

func eligibilityMask(g *grid.Grid, task model.Task, maps []model.TimeMap) grid.Mask {
	m := grid.NewMask(g.Len())
	if len(task.TimeMaps) > 0 {
		for _, slug := range task.TimeMaps {
			for _, tm := range maps {
				if tm.Slug == slug {
					m.OrInto(g.TimeMapMask(tm))
				}
			}
		}
		return m
	}
	for _, tm := range maps {
		if tm.Default {
			m.OrInto(g.TimeMapMask(tm))
		}
	}
	return m
}

// slotRange returns the grid slot interval [first, last) a Block occupies.
func slotRange(t *testing.T, g *grid.Grid, b model.Block) (first, last int) {
	t.Helper()
	for i := 0; i < g.Len(); i++ {
		if g.Start(i).Equal(b.Start) {
			return i, i + b.Slots
		}
	}
	t.Fatalf("block %+v does not start on a slot boundary", b)
	return 0, 0
}

func slotCount(d, granularity time.Duration) int {
	return int((d + granularity - 1) / granularity)
}
