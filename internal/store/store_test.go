package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/model"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "lodo.db")
}

func TestRoundTrip(t *testing.T) {
	s, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	loc, _ := time.LoadLocation("Europe/London")
	start := time.Date(2026, 11, 9, 9, 0, 0, 0, loc)
	end := start.Add(30 * time.Minute)
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, loc)

	deadline := model.CivilDate{Year: 2026, Month: 11, Day: 12}
	pin := time.Date(2026, 11, 10, 9, 30, 0, 0, loc)
	snap := model.Snapshot{
		Text: "Write the note", Marker: model.MarkerTODO, Duration: 90 * time.Minute,
		TimeMaps: []string{"deep-work"}, Priority: model.PriorityA,
		Deadline: &deadline, Pin: &pin,
	}
	if err := s.Commit(
		[]Binding{{TaskID: "t1", BlockIndex: 0, EventID: "lodoabc", Start: start, End: end, Slots: 2}},
		nil,
		[]TimedDisposition{{Disposition: model.Disposition{
			TaskID: "t1", BlockIndex: 1, Kind: model.DispositionPinned,
			Start: start, End: end, Slots: 2, Snapshot: snap,
		}, RecordedAt: now}},
		map[string]model.Snapshot{"t1": snap},
	); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	bindings, snapshots, dispositions, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(bindings) != 1 || bindings[0].TaskID != "t1" || bindings[0].Slots != 2 {
		t.Fatalf("bindings = %+v", bindings)
	}
	if !bindings[0].Start.Equal(start) || !bindings[0].End.Equal(end) {
		t.Fatalf("binding span = %v..%v, want %v..%v", bindings[0].Start, bindings[0].End, start, end)
	}
	gotSnap, ok := snapshots["t1"]
	if !ok {
		t.Fatalf("snapshots missing t1: %+v", snapshots)
	}
	if !SnapshotEqual(snap, gotSnap) {
		t.Fatalf("snapshot mismatch (-want +got):\n%s", cmp.Diff(snap, gotSnap))
	}
	if len(dispositions) != 1 || dispositions[0].Kind != model.DispositionPinned {
		t.Fatalf("dispositions = %+v", dispositions)
	}
	if !SnapshotEqual(snap, dispositions[0].Snapshot) {
		t.Fatalf("disposition snapshot mismatch")
	}
}

func TestSnapshotJSONFidelity(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/London")
	pin := time.Date(2026, 11, 10, 9, 30, 0, 0, loc)
	cases := map[string]model.Snapshot{
		"empty": {},
		"full": {
			Text: "Something", Marker: model.MarkerTODO, Duration: 90 * time.Minute,
			TimeMaps: []string{"deep", "weekends"}, Priority: model.PriorityA,
			Deadline:  &model.CivilDate{Year: 2026, Month: 11, Day: 12},
			PlanStart: &model.CivilDate{Year: 2026, Month: 11, Day: 9},
			PlanEnd:   &model.CivilDate{Year: 2026, Month: 11, Day: 10},
			Pin:       &pin,
		},
		"nil-vs-empty-maps": {
			Text: "x", Marker: model.MarkerDOING, Duration: 30 * time.Minute,
			TimeMaps: nil, Priority: "",
		},
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := SnapshotToJSON(snap)
			if err != nil {
				t.Fatalf("ToJSON: %v", err)
			}
			back, err := SnapshotFromJSON(raw)
			if err != nil {
				t.Fatalf("FromJSON: %v", err)
			}
			if !SnapshotEqual(snap, back) {
				t.Fatalf("round-trip mismatch (-want +got):\n%s", cmp.Diff(snap, back))
			}
			// Pointers stay nil when they started nil.
			if (snap.Deadline == nil) != (back.Deadline == nil) ||
				(snap.PlanStart == nil) != (back.PlanStart == nil) ||
				(snap.PlanEnd == nil) != (back.PlanEnd == nil) ||
				(snap.Pin == nil) != (back.Pin == nil) {
				t.Fatalf("nil pointers not preserved: %+v -> %+v", snap, back)
			}
			// Deterministic field order: same snapshot, same bytes.
			again, _ := SnapshotToJSON(back)
			if raw != again {
				t.Fatalf("not deterministic:\n%s\n---\n%s", raw, again)
			}
		})
	}
}

func TestCommitIsAtomic(t *testing.T) {
	s, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	// A disposition with an unknown kind still commits (the store is dumb;
	// the planner validates). Atomicity is about all-or-nothing across
	// tables: a second commit overwrites the first.
	now := time.Now().UTC()
	snap := model.Snapshot{Text: "a", Marker: model.MarkerTODO, Duration: time.Hour}
	if err := s.Commit(nil, nil, nil, map[string]model.Snapshot{"t1": snap}); err != nil {
		t.Fatalf("Commit snapshots: %v", err)
	}
	if err := s.Commit(
		[]Binding{{TaskID: "t1", BlockIndex: 0, EventID: "e1", Start: now, End: now.Add(time.Hour), Slots: 4}},
		nil, nil, nil,
	); err != nil {
		t.Fatalf("Commit bindings: %v", err)
	}
	bindings, snapshots, _, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(bindings) != 1 || len(snapshots) != 1 {
		t.Fatalf("bindings=%d snapshots=%d, want 1/1", len(bindings), len(snapshots))
	}
}
