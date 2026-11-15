package planner

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/model"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// traceInput is the kitchen-sink fixture that drives every explain-decision
// state, plus the placement paths that feed them. TestTraceFixtureCoversStates
// asserts the intent so the golden cannot silently bless a scenario that
// stopped exercising its branch.
func traceInput() Input {
	deep := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{
		on(time.Monday, 9, 0, 12, 0),
		on(time.Tuesday, 9, 0, 12, 0),
		on(time.Wednesday, 9, 0, 12, 0),
		on(time.Thursday, 9, 0, 12, 0),
		on(time.Friday, 9, 0, 12, 0),
	}}
	yellow := model.TimeMap{Slug: "yellow", Windows: []model.Window{
		onTier(time.Monday, 9, 0, 10, 0, model.TierYellow),
	}}

	placed := localTask("placed", duration(60))

	bound := localTask("bound", duration(30))
	endTue := civil(10)
	bound.PlanEnd = &endTue

	pin := localTask("pin", duration(30))
	pinAt := at(9, 8, 0)
	pin.Pin = &pinAt
	pin.Depends = []string{"placed"}

	container := localTask("container", duration(60))
	container.Container = true

	satisfied := localTask("satisfied", duration(60))
	startWed := civil(11)
	satisfied.PlanStart = &startWed

	mixed := localTask("mixed", duration(120))

	partial := localTask("partial", duration(240))
	endMon := civil(9)
	partial.PlanEnd = &endMon

	none := localTask("none", duration(60))
	startSat := civil(14)
	endSat := civil(14)
	none.PlanStart = &startSat
	none.PlanEnd = &endSat

	depNone := localTask("dep-on-none", duration(30))
	depNone.Depends = []string{"none"}

	cycA := localTask("cyc-a", duration(30))
	cycA.Depends = []string{"cyc-b"}
	cycB := localTask("cyc-b", duration(30))
	cycB.Depends = []string{"cyc-a"}
	cycTail := localTask("cyc-tail", duration(30))
	cycTail.Depends = []string{"cyc-a"}

	unknown := localTask("unknown", duration(30))
	unknown.Depends = []string{"ghost"}

	beyond := localTask("beyond", duration(60))
	startNext := civil(16)
	beyond.PlanStart = &startNext

	spill := localTask("spill", duration(120))
	spill.TimeMaps = []string{"yellow"}

	in := newInput(
		placed, bound, pin, container, satisfied, mixed, partial,
		none, depNone, cycA, cycB, cycTail, unknown, beyond, spill,
	)
	in.TimeMaps = []model.TimeMap{deep, yellow}
	in.Dispositions = []model.Disposition{
		{TaskID: "mixed", BlockIndex: 1, Kind: model.DispositionPinned, Start: at(9, 10, 0), End: at(9, 10, 30)},
		{TaskID: "mixed", BlockIndex: 2, Kind: model.DispositionStoodDown, Slots: 2},
		{TaskID: "satisfied", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: 4},
	}
	return in
}

func decisionByID(t *testing.T, s Schedule, id string) TaskDecision {
	t.Helper()
	for _, d := range s.Decisions {
		if d.TaskID == id {
			return d
		}
	}
	t.Fatalf("no decision for %q", id)
	return TaskDecision{}
}

// TestTraceFixtureCoversStates proves each fixture Task reaches the state its
// branch represents; the byte golden alone would not notice a scenario that
// drifted to a different reason.
func TestTraceFixtureCoversStates(t *testing.T) {
	s, err := Plan(traceInput())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	pin := decisionByID(t, s, "pin")
	if !pin.Eligibility.Pin || !pin.Eligibility.Considered || pin.Ordering.Kind != "pin" {
		t.Fatalf("pin eligibility/kind: %+v %+v", pin.Eligibility, pin.Ordering)
	}
	if !pin.Dependency.PinViolation || pin.Ordering.TieBreak != "pin-first" {
		t.Fatalf("pin violation missing: %+v %+v", pin.Dependency, pin.Ordering)
	}
	if pin.Outcome.Reason != string(ReasonPinPrecedesDependency) {
		t.Fatalf("pin outcome reason = %q", pin.Outcome.Reason)
	}

	container := decisionByID(t, s, "container")
	if !container.Eligibility.Container || container.Eligibility.Considered {
		t.Fatalf("container eligibility: %+v", container.Eligibility)
	}

	satisfied := decisionByID(t, s, "satisfied")
	if satisfied.Eligibility.Considered || satisfied.Ordering.Kind != "time-bound" ||
		satisfied.Eligibility.FlexibleSlots != 0 || satisfied.Eligibility.SkipReason == "" {
		t.Fatalf("satisfied eligibility: %+v %+v", satisfied.Eligibility, satisfied.Ordering)
	}

	placed := decisionByID(t, s, "placed")
	if !placed.Eligibility.Considered || placed.Outcome.Reason != "" || placed.Outcome.Placed == 0 {
		t.Fatalf("placed outcome: %+v %+v", placed.Eligibility, placed.Outcome)
	}

	partial := decisionByID(t, s, "partial")
	if partial.Outcome.Reason != string(ReasonPartialFit) || partial.Outcome.Shortfall == 0 {
		t.Fatalf("partial outcome: %+v", partial.Outcome)
	}

	none := decisionByID(t, s, "none")
	if none.Outcome.Reason != string(ReasonNoEligibleTime) {
		t.Fatalf("none outcome: %+v", none.Outcome)
	}

	depNone := decisionByID(t, s, "dep-on-none")
	if !depNone.Eligibility.Considered || !depNone.Dependency.Blocked ||
		depNone.Dependency.BlockReason != string(ReasonBlockedByDependency) || len(depNone.Dependency.BlockedBy) == 0 {
		t.Fatalf("dependency gate decision: %+v %+v", depNone.Eligibility, depNone.Dependency)
	}

	cycA := decisionByID(t, s, "cyc-a")
	if cycA.Eligibility.Considered || cycA.Ordering.Kind != "cycle-blocked" || !cycA.Dependency.CycleMember {
		t.Fatalf("cycle decision: %+v %+v", cycA.Eligibility, cycA.Dependency)
	}

	cycTail := decisionByID(t, s, "cyc-tail")
	if cycTail.Eligibility.Considered || cycTail.Ordering.Kind != "blocked" ||
		cycTail.Dependency.BlockReason != string(ReasonBlockedByDependency) {
		t.Fatalf("downstream decision: %+v %+v", cycTail.Eligibility, cycTail.Dependency)
	}

	unknown := decisionByID(t, s, "unknown")
	if unknown.Ordering.Kind != "blocked" || unknown.Dependency.BlockReason != string(ReasonUnknownDependency) ||
		len(unknown.Dependency.BlockedBy) != 1 || unknown.Dependency.BlockedBy[0] != "ghost" {
		t.Fatalf("unknown dependency decision: %+v", unknown.Dependency)
	}

	beyond := decisionByID(t, s, "beyond")
	if !beyond.Eligibility.BeyondHorizon || beyond.Eligibility.Considered || beyond.Ordering.TieBreak != "beyond-horizon" {
		t.Fatalf("beyond horizon decision: %+v %+v", beyond.Eligibility, beyond.Ordering)
	}

	mixed := decisionByID(t, s, "mixed")
	if len(mixed.Dispositions) != 2 {
		t.Fatalf("mixed dispositions: %+v", mixed.Dispositions)
	}

	spill := decisionByID(t, s, "spill")
	if !spill.TimeMaps.NoUpwardSpill || len(spill.TimeMaps.Passes) == 0 || spill.Outcome.Shortfall == 0 {
		t.Fatalf("spill decision: %+v %+v", spill.TimeMaps, spill.Outcome)
	}
}

// goldenSchedule is Schedule with the decision dispositions stripped. A
// model.Disposition carries a Snapshot whose time.Duration has no default
// representation in encoding/json/v2, the encoder explain --json uses.
type goldenSchedule struct {
	Blocks       []model.Block
	Report       Report
	Explanations []Explanation
	Decisions    []TaskDecision
	Suggestions  []Suggestion
}

// TestScheduleGolden pins the whole planner output, including the explain
// surface, byte for byte. It was generated before the decision-recorder
// refactor and must not be regenerated as part of it.
func TestScheduleGolden(t *testing.T) {
	s, err := Plan(traceInput())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	decisions := make([]TaskDecision, len(s.Decisions))
	for i, d := range s.Decisions {
		d.Dispositions = nil
		decisions[i] = d
	}
	raw, err := jsonx.MarshalIndent(goldenSchedule{
		Blocks:       s.Blocks,
		Report:       s.Report,
		Explanations: s.Explanations,
		Decisions:    decisions,
		Suggestions:  s.Suggestions,
	}, "  ")
	if err != nil {
		t.Fatalf("marshal schedule: %v", err)
	}
	got := append(raw, '\n')

	path := filepath.Join("testdata", "trace.golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("schedule golden mismatch; run with -update only for an intended output change\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
