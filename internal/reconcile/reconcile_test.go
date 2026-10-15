package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/store"
)

var london = mustLocation("Europe/London")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func at(day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, london)
}

func managed(id, task string, block int, start, end time.Time) calendar.Event {
	return calendar.Event{
		ID:      id,
		Summary: "Task " + task,
		Start:   calendar.EventTime{DateTime: start.Format(time.RFC3339)},
		End:     calendar.EventTime{DateTime: end.Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": task, "block": itoa(block),
		}},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

func TestEventIDStabilityAndCharset(t *testing.T) {
	a := EventID("t-task-uuid", 0)
	b := EventID("t-task-uuid", 0)
	c := EventID("t-task-uuid", 1)
	if a != b {
		t.Fatalf("not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("collision across block index: %q", a)
	}
	for _, id := range []string{a, c, EventID("other", 12)} {
		if !strings.HasPrefix(id, "lodo") {
			t.Fatalf("%q has no lodo prefix", id)
		}
		for _, ch := range id {
			allowed := ch == '-' ||
				(ch >= 'a' && ch <= 'z') ||
				(ch >= '0' && ch <= '9')
			if !allowed {
				t.Fatalf("%q has illegal char %q", id, ch)
			}
		}
		if len(id) > 100 {
			t.Fatalf("%q is too long for Google", id)
		}
	}
}

func TestGoldenTable(t *testing.T) {
	granularity := 15 * time.Minute
	now := at(9, 8, 0)
	snap := model.Snapshot{Text: "Audit", Marker: model.MarkerTODO, Duration: 90 * time.Minute}
	snapshots := map[string]model.Snapshot{"t1": snap, "t2": snap}

	bindings := []store.Binding{
		{TaskID: "t1", BlockIndex: 0, EventID: "e-same", Start: at(9, 9, 0), End: at(9, 9, 30), Slots: 2},
		{TaskID: "t1", BlockIndex: 1, EventID: "e-moved", Start: at(9, 10, 0), End: at(9, 10, 30), Slots: 2},
		{TaskID: "t2", BlockIndex: 0, EventID: "e-gone", Start: at(9, 11, 0), End: at(9, 11, 30), Slots: 2},
	}
	observed := []calendar.Event{
		managed("e-same", "t1", 0, at(9, 9, 0), at(9, 9, 30)),    // unchanged → nothing
		managed("e-moved", "t1", 1, at(9, 14, 0), at(9, 14, 30)), // moved → pinned at observed
		managed("e-new", "t3", 0, at(9, 15, 0), at(9, 15, 30)),   // adopted → binding, no snapshot
	}

	res, err := Reconcile(observed, bindings, snapshots, granularity, london, now)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Moved: binding follows the observed span.
	var moved *store.Binding
	for _, b := range res.BindingUpserts {
		if b.EventID == "e-moved" {
			b := b
			moved = &b
		}
	}
	if moved == nil || !moved.Start.Equal(at(9, 14, 0)) || !moved.End.Equal(at(9, 14, 30)) {
		t.Fatalf("moved binding = %+v, want the observed span", res.BindingUpserts)
	}
	// Adopted: binding with no disposition.
	var adopted *store.Binding
	for _, b := range res.BindingUpserts {
		if b.EventID == "e-new" {
			b := b
			adopted = &b
		}
	}
	if adopted == nil || adopted.TaskID != "t3" || adopted.Slots != 2 {
		t.Fatalf("adopted binding = %+v", res.BindingUpserts)
	}
	// Deleted: binding gone, stood-down with the binding's slots and the
	// stored snapshot.
	if len(res.BindingDeletes) != 1 || res.BindingDeletes[0].TaskID != "t2" {
		t.Fatalf("deletes = %+v, want t2/0", res.BindingDeletes)
	}
	var stood, pinned *store.TimedDisposition
	for _, d := range res.DispositionUpserts {
		d := d
		switch d.Disposition.Kind {
		case model.DispositionStoodDown:
			stood = &d
		case model.DispositionPinned:
			pinned = &d
		}
	}
	if stood == nil || stood.Disposition.TaskID != "t2" || stood.Disposition.Slots != 2 {
		t.Fatalf("stood-down = %+v", res.DispositionUpserts)
	}
	if !store.SnapshotEqual(snap, stood.Disposition.Snapshot) {
		t.Fatalf("stood-down snapshot = %+v, want the stored one", stood.Disposition.Snapshot)
	}
	if pinned == nil || !pinned.Disposition.Start.Equal(at(9, 14, 0)) {
		t.Fatalf("pinned = %+v, want the observed span", res.DispositionUpserts)
	}
	// Unchanged produced nothing.
	for _, b := range res.BindingUpserts {
		if b.EventID == "e-same" {
			t.Fatalf("unchanged event produced an upsert: %+v", b)
		}
	}
	// Adopted produced no disposition.
	for _, d := range res.DispositionUpserts {
		if d.Disposition.TaskID == "t3" {
			t.Fatalf("adopted produced a disposition: %+v", d)
		}
	}
}

func TestAdoptedThenDeletedMatchesNothing(t *testing.T) {
	granularity := 15 * time.Minute
	now := at(9, 8, 0)
	// Adoption wrote no snapshot; the task still has none stored.
	res, err := Reconcile(nil,
		[]store.Binding{{TaskID: "t9", BlockIndex: 0, EventID: "e-orphan", Start: at(9, 9, 0), End: at(9, 9, 30), Slots: 2}},
		map[string]model.Snapshot{}, granularity, london, now)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(res.DispositionUpserts) != 1 {
		t.Fatalf("dispositions = %+v, want one stood-down", res.DispositionUpserts)
	}
	got := res.DispositionUpserts[0].Disposition
	if got.Kind != model.DispositionStoodDown || !store.SnapshotEqual(model.Snapshot{}, got.Snapshot) {
		t.Fatalf("want an empty snapshot that matches nothing, got %+v", got)
	}
	// An empty snapshot matches no real task: the Block is re-created once.
	tasks := []model.Task{{ID: "t9", Text: "Back", Marker: model.MarkerTODO, Duration: 30 * time.Minute}}
	if eff := Effective(tasks, []model.Disposition{got}); len(eff) != 0 {
		t.Fatalf("empty snapshot must not hold the Block down: %+v", eff)
	}
}

func TestEffectiveFiltersBySnapshot(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Audit", Marker: model.MarkerTODO, Duration: 90 * time.Minute}
	snap := task.Snapshot()
	pinned := model.Disposition{TaskID: "t1", BlockIndex: 0, Kind: model.DispositionPinned, Start: at(9, 9, 0), End: at(9, 9, 30)}
	stood := model.Disposition{TaskID: "t1", BlockIndex: 1, Kind: model.DispositionStoodDown, Slots: 2, Snapshot: snap}

	// Both apply while inputs match.
	if eff := Effective([]model.Task{task}, []model.Disposition{pinned, stood}); len(eff) != 2 {
		t.Fatalf("effective = %+v, want both", eff)
	}
	// A duration change releases the stood-down Block but keeps the pin:
	// pins survive unrelated input changes until deleted.
	task.Duration = 120 * time.Minute
	eff := Effective([]model.Task{task}, []model.Disposition{pinned, stood})
	if len(eff) != 1 || eff[0].Kind != model.DispositionPinned {
		t.Fatalf("effective after input change = %+v, want only the pin", eff)
	}
	// Unknown tasks drop their rows here; check still lists the stored ones.
	if eff := Effective(nil, []model.Disposition{stood}); len(eff) != 0 {
		t.Fatalf("effective for unknown task = %+v, want none", eff)
	}
}

func TestUnchangedRunKeepsDispositions(t *testing.T) {
	granularity := 15 * time.Minute
	now := at(9, 8, 0)
	b := store.Binding{TaskID: "t1", BlockIndex: 0, EventID: "e1", Start: at(9, 9, 0), End: at(9, 9, 30), Slots: 2}
	obs := []calendar.Event{managed("e1", "t1", 0, at(9, 9, 0), at(9, 9, 30))}
	res, err := Reconcile(obs, []store.Binding{b}, map[string]model.Snapshot{}, granularity, london, now)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(res.BindingUpserts) != 0 || len(res.BindingDeletes) != 0 || len(res.DispositionUpserts) != 0 {
		t.Fatalf("unchanged run must be empty: %+v", res)
	}
}

func TestOrphanPointersAreSkipped(t *testing.T) {
	bad := calendar.Event{
		ID:      "e-bad",
		Summary: "mystery",
		Start:   calendar.EventTime{DateTime: at(9, 9, 0).Format(time.RFC3339)},
		End:     calendar.EventTime{DateTime: at(9, 9, 30).Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", // no task/block pointer
		}},
	}
	res, err := Reconcile([]calendar.Event{bad}, nil, nil, 15*time.Minute, london, at(9, 8, 0))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if diff := cmp.Diff(Result{}, res); diff != "" {
		t.Fatalf("orphan pointer must be left for check, got (-want +got):\n%s", diff)
	}
	if _, _, ok := TaskRef(bad); ok {
		t.Fatalf("TaskRef(bad) must not be ok")
	}
}
