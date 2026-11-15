package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/jsonx"
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
			Graph: "work",
			Text:  "Something", Marker: model.MarkerTODO, Duration: 90 * time.Minute,
			TimeMaps: []string{"deep", "weekends"}, Priority: model.PriorityA,
			Deadline:  &model.CivilDate{Year: 2026, Month: 11, Day: 12},
			PlanStart: &model.CivilDate{Year: 2026, Month: 11, Day: 9},
			PlanEnd:   &model.CivilDate{Year: 2026, Month: 11, Day: 10},
			Pin:       &pin,
			Depends:   []string{"dep-a", "dep-b"},
			Order:     model.OrderSequential,
		},
		"nil-vs-empty-maps": {
			Text: "x", Marker: model.MarkerDOING, Duration: 30 * time.Minute,
			TimeMaps: nil, Priority: "",
		},
		"empty-depends-and-order": {
			Text: "x", Marker: model.MarkerTODO, Duration: time.Hour,
			Depends: []string{}, Order: model.OrderParallel,
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

func TestRunRecord(t *testing.T) {
	s, err := Open(tempPath(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	now := time.Now().UTC()
	if err := s.RecordRun("schedule", "primary", 5,
		[]RunSource{{CalendarID: "a@example.com", Status: "read", Events: 3}}, now); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if err := s.RecordRun("refresh", "primary", 5,
		[]RunSource{{CalendarID: "bad@example.com", Status: "skipped:403", Events: 0}}, now); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	rec, err := s.LastRun()
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if rec.Command != "refresh" || rec.WriteCalendar != "primary" || rec.WriteEvents != 5 {
		t.Fatalf("last run = %+v", rec)
	}
	if len(rec.Sources) != 1 || rec.Sources[0].Status != "skipped:403" {
		t.Fatalf("sources = %+v", rec.Sources)
	}
}

func TestBindingKeyPromotion(t *testing.T) {
	b := Binding{TaskID: "task", BlockIndex: 2, EventID: "event"}
	if b.BindingKey != (BindingKey{TaskID: "task", BlockIndex: 2}) {
		t.Fatalf("promoted fields did not initialize key: %+v", b)
	}
	raw, err := jsonx.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"TaskID":"task","BlockIndex":2,"EventID":"event","Start":"0001-01-01T00:00:00Z","End":"0001-01-01T00:00:00Z","Slots":0}`
	if string(raw) != want {
		t.Fatalf("binding JSON changed: %s", raw)
	}
	var back Binding
	if err := jsonx.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back != b {
		t.Fatalf("binding round-trip = %+v, want %+v", back, b)
	}
}

func TestBindingKeyCompare(t *testing.T) {
	key := BindingKey{TaskID: "task", BlockIndex: 2}
	tests := []struct {
		other BindingKey
		want  int
	}{
		{key, 0},
		{BindingKey{TaskID: "task", BlockIndex: 1}, 1},
		{BindingKey{TaskID: "task", BlockIndex: 3}, -1},
		{BindingKey{TaskID: "before", BlockIndex: 99}, 1},
		{BindingKey{TaskID: "later", BlockIndex: 0}, 1},
		{BindingKey{TaskID: "zz", BlockIndex: 0}, -1},
	}
	for _, tt := range tests {
		if got := key.Compare(tt.other); got != tt.want {
			t.Fatalf("Compare(%+v) = %d, want %d", tt.other, got, tt.want)
		}
		if got := tt.other.Compare(key); got != -tt.want {
			t.Fatalf("reverse Compare = %d, want %d", got, -tt.want)
		}
	}
}

func TestCommitRollsBackOnError(t *testing.T) {
	tests := []struct {
		name string
		fail func(*Store, Binding) error
	}{
		{"missing identity", func(s *Store, binding Binding) error {
			return s.Commit([]Binding{binding, {TaskID: "invalid"}}, nil, nil, nil)
		}},
		{"duplicate event", func(s *Store, binding Binding) error {
			return s.Commit([]Binding{binding, {TaskID: "duplicate", EventID: binding.EventID}}, nil, nil, nil)
		}},
		{"invalid snapshot", func(s *Store, binding Binding) error {
			pin := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
			return s.Commit([]Binding{binding}, nil,
				[]TimedDisposition{{Disposition: model.Disposition{TaskID: "task", Kind: model.DispositionStoodDown, Slots: 1}}},
				map[string]model.Snapshot{"task": {Pin: &pin}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Open(tempPath(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			key := BindingKey{TaskID: "task", BlockIndex: 0}
			if err := s.SetPendingDeletes([]BindingKey{key}); err != nil {
				t.Fatal(err)
			}
			binding := Binding{BindingKey: key, EventID: "event", Start: time.Date(2026, time.November, 9, 9, 0, 0, 0, time.UTC), Slots: 1}
			binding.End = binding.Start.Add(15 * time.Minute)
			if err := tt.fail(s, binding); err == nil {
				t.Fatal("invalid commit succeeded")
			}
			bindings, snapshots, dispositions, err := s.All()
			if err != nil {
				t.Fatal(err)
			}
			if len(bindings) != 0 || len(snapshots) != 0 || len(dispositions) != 0 {
				t.Fatalf("partial commit: bindings=%v snapshots=%v dispositions=%v", bindings, snapshots, dispositions)
			}
			pending, err := s.PendingDeletes()
			if err != nil || len(pending) != 1 || pending[0] != key {
				t.Fatalf("failed commit lost pending intent: %v, %v", pending, err)
			}
			if err := s.Commit([]Binding{binding}, nil, nil, nil); err != nil {
				t.Fatalf("commit after rollback: %v", err)
			}
			pending, err = s.PendingDeletes()
			if err != nil || len(pending) != 0 {
				t.Fatalf("successful commit did not clear pending intent: %v, %v", pending, err)
			}
		})
	}
}

func TestPendingDeletesRollBackOnError(t *testing.T) {
	s, err := Open(tempPath(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	key := BindingKey{TaskID: "task", BlockIndex: 1}
	if err := s.SetPendingDeletes([]BindingKey{key, {BlockIndex: 2}}); err == nil {
		t.Fatal("invalid pending delete succeeded")
	}
	if pending, err := s.PendingDeletes(); err != nil || len(pending) != 0 {
		t.Fatalf("partial pending deletes: %v, %v", pending, err)
	}
	if err := s.SetPendingDeletes([]BindingKey{key}); err != nil {
		t.Fatalf("pending delete after rollback: %v", err)
	}
	if err := s.Commit(nil, []BindingKey{key}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingDeletes(); err != nil || len(pending) != 0 {
		t.Fatalf("delete commit did not clear pending intent: %v, %v", pending, err)
	}
}
