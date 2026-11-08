package planner

import (
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

func gridFor(in Input) *grid.Grid {
	return grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)
}

func TestDecisionsReflectActualPlacement(t *testing.T) {
	// Placed flexible task: has time-map/tier passes and comparator decision.
	in := newInput(localTask("t1", duration(90)))
	in.TimeMaps = []model.TimeMap{deepMap(
		onTier(time.Monday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Monday, 13, 0, 14, 0, model.TierGreen),
	)}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Decisions) != 1 {
		t.Fatalf("decisions=%d want 1", len(s.Decisions))
	}
	d := s.Decisions[0]
	if d.TaskID != "t1" || !d.Eligibility.Considered || d.Eligibility.FlexibleSlots != 6 {
		t.Fatalf("eligibility: %+v", d.Eligibility)
	}
	if d.TimeMaps.BestTier != "green" || len(d.TimeMaps.Passes) == 0 {
		t.Fatalf("time maps: %+v", d.TimeMaps)
	}
	foundComparator := false
	for _, p := range d.TimeMaps.Passes {
		if p.Comparator != nil && p.SlotsPlaced > 0 {
			foundComparator = true
			if p.Comparator.Completeness != p.SlotsPlaced {
				t.Fatalf("completeness: %+v", p.Comparator)
			}
		}
	}
	if !foundComparator {
		t.Fatalf("expected comparator decision in passes: %+v", d.TimeMaps.Passes)
	}
	if d.Outcome.Placed != 6 || d.Outcome.Shortfall != 0 {
		t.Fatalf("outcome: %+v", d.Outcome)
	}
}

func TestDecisionsDependencyBlocked(t *testing.T) {
	a := localTask("a", duration(120))
	b := depTask("b", duration(30), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 9, 30))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var db *TaskDecision
	for i := range s.Decisions {
		if s.Decisions[i].TaskID == "b" {
			db = &s.Decisions[i]
			break
		}
	}
	if db == nil {
		t.Fatalf("missing b decision: %+v", s.Decisions)
	}
	if !db.Dependency.Blocked || db.Dependency.BlockReason == "" {
		t.Fatalf("dependency gate: %+v", db.Dependency)
	}
	if db.Outcome.Reason == "" {
		t.Fatalf("outcome: %+v", db.Outcome)
	}
}

func TestDecisionsBeyondHorizon(t *testing.T) {
	task := localTask("future", duration(60))
	start := civil(16)
	task.PlanStart = &start
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Decisions) != 1 {
		t.Fatalf("decisions=%d", len(s.Decisions))
	}
	d := s.Decisions[0]
	if !d.Eligibility.BeyondHorizon || d.Eligibility.Considered {
		t.Fatalf("eligibility: %+v", d.Eligibility)
	}
}

func TestDecisionsContainer(t *testing.T) {
	a := localTask("a", duration(60))
	a.Container = true
	b := localTask("b", duration(30))
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var da *TaskDecision
	for i := range s.Decisions {
		if s.Decisions[i].TaskID == "a" {
			da = &s.Decisions[i]
		}
	}
	if da == nil || !da.Eligibility.Container || da.Eligibility.Considered {
		t.Fatalf("container decision: %+v", da)
	}
}

func TestDecisionsTierLaterGreenBeatsEarlierYellow(t *testing.T) {
	in := newInput(localTask("t1", duration(120)))
	in.TimeMaps = []model.TimeMap{deepMap(
		onTier(time.Monday, 9, 0, 11, 0, model.TierYellow),
		onTier(time.Friday, 9, 0, 11, 0, model.TierGreen),
	)}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	d := s.Decisions[0]
	// Should have placed in green pass.
	greenPlaced := 0
	for _, p := range d.TimeMaps.Passes {
		if p.Tier == "green" {
			greenPlaced += p.SlotsPlaced
		}
	}
	if greenPlaced != 8 {
		t.Fatalf("expected 8 slots placed in green, got %d; passes=%+v", greenPlaced, d.TimeMaps.Passes)
	}
}

func TestDecisionsTieBreakOutlineOrder(t *testing.T) {
	a := localTask("a", duration(30))
	a.OutlineOrder = 1
	b := localTask("b", duration(30))
	b.OutlineOrder = 2
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	byID := map[string]TaskDecision{}
	for _, d := range s.Decisions {
		byID[d.TaskID] = d
	}
	da, db := byID["a"], byID["b"]
	if da.Ordering.Position != 1 || db.Ordering.Position != 2 || da.Ordering.Total != 2 {
		t.Fatalf("positions: a=%+v b=%+v", da.Ordering, db.Ordering)
	}
	if da.Ordering.TieBreak != "outline-order" {
		t.Fatalf("a tie-break=%q detail=%q, want outline-order", da.Ordering.TieBreak, da.Ordering.TieBreakDetail)
	}
	if da.Ordering.TieBreakDetail == "" || db.Ordering.TieBreak == "" {
		t.Fatalf("tie-break detail missing: a=%+v b=%+v", da.Ordering, db.Ordering)
	}
	if db.Ordering.TieBreak != "only-ready" {
		t.Fatalf("b tie-break=%q, want only-ready (sole ready peer after a)", db.Ordering.TieBreak)
	}
}

func TestDecisionsTieBreakDependency(t *testing.T) {
	a := localTask("a", duration(30))
	b := depTask("b", duration(30), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, d := range s.Decisions {
		if d.TaskID == "b" {
			if d.Ordering.TieBreak != "dependency" {
				t.Fatalf("b tie-break=%q detail=%q, want dependency", d.Ordering.TieBreak, d.Ordering.TieBreakDetail)
			}
			if d.Ordering.Position != 2 || d.Ordering.Total != 2 {
				t.Fatalf("b position=%+v", d.Ordering)
			}
		}
	}
}

func TestDecisionsRejectionBreakdownSums(t *testing.T) {
	task := localTask("t1", duration(60))
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	g := gridFor(in)
	in.Busy = g.Overlapping(at(9, 9, 30), at(9, 10, 0))
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	d := s.Decisions[0]
	if len(d.TimeMaps.Passes) == 0 {
		t.Fatalf("no passes: %+v", d.TimeMaps)
	}
	for _, p := range d.TimeMaps.Passes {
		if p.RejectedBusy != p.RejectedExternal+p.RejectedPin+p.RejectedPinned+p.RejectedPlaced {
			t.Fatalf("busy breakdown must sum: %+v", p)
		}
		if p.RejectedOutOfWindow != p.RejectedBeforeBound+p.RejectedOutsidePlan {
			t.Fatalf("window breakdown must sum: %+v", p)
		}
	}
	foundExternal := false
	for _, p := range d.TimeMaps.Passes {
		if p.RejectedExternal > 0 {
			foundExternal = true
		}
	}
	if !foundExternal {
		t.Fatalf("expected external rejection from busy mask: %+v", d.TimeMaps.Passes)
	}
	if d.Placement == nil || d.Placement.RequiredSlots != 4 {
		t.Fatalf("placement required slots: %+v", d.Placement)
	}
}

func TestDecisionsPinBreakdown(t *testing.T) {
	pinned := localTask("pinned", duration(30))
	pinned.Pin = &[]time.Time{at(9, 9, 30)}[0]
	other := localTask("other", duration(60))
	in := newInput(pinned, other)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var dOther *TaskDecision
	for i := range s.Decisions {
		if s.Decisions[i].TaskID == "other" {
			dOther = &s.Decisions[i]
		}
	}
	if dOther == nil {
		t.Fatalf("missing other decision")
	}
	foundPin := false
	for _, p := range dOther.TimeMaps.Passes {
		if p.RejectedPin > 0 {
			foundPin = true
		}
	}
	if !foundPin {
		t.Fatalf("expected pin rejection in passes: %+v", dOther.TimeMaps.Passes)
	}
}

func TestDecisionsDispositionsExposed(t *testing.T) {
	task := localTask("t1", duration(120))
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	in.Dispositions = []model.Disposition{{
		TaskID: "t1", BlockIndex: 1, Kind: model.DispositionPinned,
		Start: at(9, 9, 30), End: at(9, 10, 0),
	}}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	d := s.Decisions[0]
	if len(d.Dispositions) != 1 || d.Dispositions[0].Kind != model.DispositionPinned {
		t.Fatalf("dispositions: %+v", d.Dispositions)
	}
}
