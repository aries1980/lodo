package run_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/run"
	"github.com/aries1980/lodo/internal/store"
)

// fakeCal is an httptest Calendar API: list plus insert/patch/delete,
// recording every mutating call for the idempotence gate.
type fakeCal struct {
	mu     sync.Mutex
	events map[string]calendar.Event
	writes []string // "POST id", "PATCH id", "DELETE id" in order
	lists  int
}

func newFakeCal() *fakeCal {
	return &fakeCal{events: map[string]calendar.Event{}}
}

func (f *fakeCal) mount(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.Contains(path, "/events") && !strings.Contains(path, "/events/"):
			f.lists++
			items := make([]calendar.Event, 0, len(f.events))
			for _, e := range f.events {
				items = append(items, e)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/events"):
			body, _ := io.ReadAll(r.Body)
			var e calendar.Event
			if err := json.Unmarshal(body, &e); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if e.ID == "" {
				http.Error(w, "missing id", http.StatusBadRequest)
				return
			}
			f.writes = append(f.writes, "POST "+e.ID)
			f.events[e.ID] = e
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(e)
		case (r.Method == http.MethodPatch || r.Method == http.MethodPut) && strings.Contains(path, "/events/"):
			id := path[strings.LastIndex(path, "/")+1:]
			body, _ := io.ReadAll(r.Body)
			var patch calendar.Event
			if err := json.Unmarshal(body, &patch); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			cur, ok := f.events[id]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"no such event"}}`))
				return
			}
			f.writes = append(f.writes, "PATCH "+id)
			if patch.Summary != "" {
				cur.Summary = patch.Summary
			}
			if patch.Start.DateTime != "" {
				cur.Start = patch.Start
			}
			if patch.End.DateTime != "" {
				cur.End = patch.End
			}
			f.events[id] = cur
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cur)
		case r.Method == http.MethodDelete && strings.Contains(path, "/events/"):
			id := path[strings.LastIndex(path, "/")+1:]
			f.writes = append(f.writes, "DELETE "+id)
			if _, ok := f.events[id]; !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"no such event"}}`))
				return
			}
			delete(f.events, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeCal) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func (f *fakeCal) resetWrites() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = nil
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "lodo.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func calendarClient(t *testing.T, base string) *calendar.Client {
	t.Helper()
	c, err := calendar.NewClient(nil, "primary", base)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestIdempotenceGate(t *testing.T) {
	// One 30m task; the first schedule inserts one event, the second issues
	// zero calendar writes and the same store contents.
	fc := newFakeCal()
	base := fc.mount(t)
	cal := calendarClient(t, base)
	st := openTestStore(t)
	cfg := testConfig()
	now := at(9, 8, 0)
	ctx := context.Background()

	client := newStubClient(busyGraph())
	first, err := run.Schedule(ctx, client, cal, st, cfg, now)
	if err != nil {
		t.Fatalf("first Schedule: %v", err)
	}
	if len(first.Writes) == 0 {
		t.Fatalf("first schedule wrote nothing; want the insert")
	}
	if fc.writeCount() == 0 {
		t.Fatalf("fake calendar saw no mutating calls")
	}
	bindings1, _, disps1, err := st.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	fc.resetWrites()
	second, err := run.Schedule(ctx, newStubClient(busyGraph()), cal, st, cfg, now)
	if err != nil {
		t.Fatalf("second Schedule: %v", err)
	}
	if len(second.Writes) != 0 {
		t.Fatalf("second schedule writes = %+v, want zero (no-op)", second.Writes)
	}
	if got := fc.writeCount(); got != 0 {
		t.Fatalf("second run issued %d calendar writes, want 0", got)
	}
	bindings2, _, disps2, err := st.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if diff := cmp.Diff(bindings1, bindings2); diff != "" {
		t.Fatalf("store bindings changed on a no-op (-first +second):\n%s", diff)
	}
	if diff := cmp.Diff(disps1, disps2); diff != "" {
		t.Fatalf("store dispositions changed on a no-op (-first +second):\n%s", diff)
	}
}

func TestCrashRecoveryAdoptThenNoOp(t *testing.T) {
	// Seed a store-missing, calendar-present managed event (the
	// calendar-written/store-crashed gap). Refresh adopts it; the next
	// schedule reproduces the same times, adopt-then-no-op.
	fc := newFakeCal()
	base := fc.mount(t)
	cal := calendarClient(t, base)
	st := openTestStore(t)
	cfg := testConfig()
	now := at(9, 8, 0)
	ctx := context.Background()

	// The event Lodo would have written: deterministic id for t-audit/0,
	// 30m at Monday 11:00 (inside the default map, after the busyGraph task
	// places at 09:00? busyGraph's task is 30m, default map Mon-Fri
	// 09:00-12:00, now Monday 08:00 → plan puts it 09:00-09:30. Seed the
	// calendar with that exact placement to make adoption no-op.)
	taskID := "t-audit"
	eventID := reconcile.EventID(taskID, 0)
	seedStart := at(9, 9, 0)
	seedEnd := at(9, 9, 30)
	fc.mu.Lock()
	fc.events[eventID] = calendar.Event{
		ID: eventID, Summary: "Audit the ledger",
		Start: calendar.EventTime{DateTime: seedStart.Format(time.RFC3339)},
		End:   calendar.EventTime{DateTime: seedEnd.Format(time.RFC3339)},
		ExtendedProperties: calendar.Extended{Private: map[string]string{
			"lodo": "true", "task": taskID, "block": "0",
		}},
	}
	fc.mu.Unlock()

	refresh, err := run.Refresh(ctx, cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(refresh.Adopted) != 1 || refresh.Adopted[0].EventID != eventID {
		t.Fatalf("adopted = %+v, want the seeded event", refresh.Adopted)
	}
	if len(refresh.Captured) != 0 {
		t.Fatalf("captured = %+v, want no dispositions on adopt", refresh.Captured)
	}

	fc.resetWrites()
	sched, err := run.Schedule(ctx, newStubClient(busyGraph()), cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Schedule after adopt: %v", err)
	}
	// The plan reproduces the adopted times (same intent → same Schedule),
	// so no calendar writes: adopt-then-no-op.
	if len(sched.Writes) != 0 {
		t.Fatalf("schedule after adopt writes = %+v, want no-op", sched.Writes)
	}
	if got := fc.writeCount(); got != 0 {
		t.Fatalf("schedule after adopt issued %d writes, want 0", got)
	}
}

// failingStore fails the first Commit to encode the
// calendar-written/store-crashed sequence.
type failingStore struct {
	inner *store.Store
	fail  bool
}

func (f *failingStore) All() ([]store.Binding, map[string]model.Snapshot, []model.Disposition, error) {
	return f.inner.All()
}

func (f *failingStore) Commit(up []store.Binding, del []store.BindingKey, disp []store.TimedDisposition, snap map[string]model.Snapshot) error {
	if f.fail {
		f.fail = false
		return fmt.Errorf("injected commit error")
	}
	return f.inner.Commit(up, del, disp, snap)
}

func TestCalendarBeforeStoreAdoptsOnRecovery(t *testing.T) {
	fc := newFakeCal()
	base := fc.mount(t)
	cal := calendarClient(t, base)
	inner := openTestStore(t)
	st := &failingStore{inner: inner, fail: true}
	cfg := testConfig()
	now := at(9, 8, 0)
	ctx := context.Background()

	// First schedule: the calendar insert happens, then the store commit
	// fails (injected). The event exists with no local row.
	if _, err := run.Schedule(ctx, newStubClient(busyGraph()), cal, st, cfg, now); err == nil ||
		!strings.Contains(err.Error(), "injected commit error") {
		t.Fatalf("first Schedule err = %v, want the injected failure", err)
	}
	if got := fc.writeCount(); got == 0 {
		t.Fatalf("first run wrote nothing; the calendar insert must precede the store commit")
	}
	if _, _, disps, _ := inner.All(); len(disps) != 0 {
		t.Fatalf("store must still be empty after the failed commit")
	}

	// Next run adopts rather than standing down: the lodo=true event with
	// no row is "state was lost", not "user deleted".
	bindings, snapshots, _, err := inner.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)
	_, managed, _, err := cal.Busy(ctx, g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	granularity := 15 * time.Minute
	loc := london
	rec, err := reconcile.Reconcile(managed, bindings, snapshots, granularity, loc, now)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(rec.BindingUpserts) == 0 {
		t.Fatalf("recovery must adopt, got %+v", rec)
	}
	for _, d := range rec.DispositionUpserts {
		if d.Disposition.Kind == model.DispositionStoodDown {
			t.Fatalf("recovery must not stand down a crash-adopted event: %+v", rec.DispositionUpserts)
		}
	}
}

func TestStoodDownPersistsUntilInputChange(t *testing.T) {
	fc := newFakeCal()
	base := fc.mount(t)
	cal := calendarClient(t, base)
	st := openTestStore(t)
	cfg := testConfig()
	now := at(9, 8, 0)
	ctx := context.Background()

	// Schedule once so the event exists.
	if _, err := run.Schedule(ctx, newStubClient(busyGraph()), cal, st, cfg, now); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// User deletes the event on the calendar.
	fc.mu.Lock()
	for id := range fc.events {
		delete(fc.events, id)
	}
	fc.mu.Unlock()

	// Refresh folds the delete into a stood-down disposition.
	ref, err := run.Refresh(ctx, cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(ref.Captured) != 1 || ref.Captured[0].Kind != model.DispositionStoodDown {
		t.Fatalf("captured = %+v, want one stood-down", ref.Captured)
	}

	// Next schedule with unchanged inputs: the Block stays down (no insert
	// for that index).
	sched, err := run.Schedule(ctx, newStubClient(busyGraph()), cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Schedule after delete: %v", err)
	}
	for _, w := range sched.Writes {
		if w.Op == "insert" {
			t.Fatalf("stood-down Block was recreated without an input change: %+v", sched.Writes)
		}
	}

	// Drive a duration change: 30m → 60m. The planner must re-place the
	// full n (4 slots), ignoring the stale stood-down (2 slots).
	changed := busyGraph()
	for _, tree := range changed.trees {
		for _, b := range tree {
			if b.UUID == "t-audit" {
				b.Properties["duration"] = "60m"
			}
		}
	}
	sched2, err := run.Schedule(ctx, newStubClient(changed), cal, st, cfg, now)
	if err != nil {
		t.Fatalf("Schedule after input change: %v", err)
	}
	placed := 0
	for _, b := range sched2.Schedule.Blocks {
		if b.TaskID == "t-audit" {
			placed += b.Slots
		}
	}
	if placed != 4 {
		t.Fatalf("placed = %d slots, want the full 4 after the snapshot mismatch", placed)
	}
}
