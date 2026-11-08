package planner

import (
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

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
