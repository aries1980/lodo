package run

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/grid"
)

// stubCalendar satisfies the Calendar read surface; these tests only exercise
// applyWrites.
type stubCalendar struct{}

func (stubCalendar) Busy(context.Context, *grid.Grid, time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error) {
	return nil, nil, calendar.BusyReport{}, nil
}

func (stubCalendar) List(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	return nil, nil
}

func (stubCalendar) Get(context.Context, string) (calendar.Event, error) {
	return calendar.Event{}, errors.New("stubCalendar: Get not expected")
}

// concurrentInsertCal blocks every insert until the test releases it, so a
// sequential implementation can only ever have one in flight.
type concurrentInsertCal struct {
	stubCalendar
	started chan string
	release <-chan struct{}
}

func (c *concurrentInsertCal) Insert(ctx context.Context, e calendar.Event) (calendar.Event, error) {
	c.started <- e.ID
	select {
	case <-c.release:
		return e, nil
	case <-ctx.Done():
		return calendar.Event{}, ctx.Err()
	}
}

func (c *concurrentInsertCal) Patch(context.Context, string, calendar.Event) (calendar.Event, error) {
	return calendar.Event{}, errors.New("concurrentInsertCal: patch not expected")
}

func (c *concurrentInsertCal) Delete(context.Context, string) error {
	return errors.New("concurrentInsertCal: delete not expected")
}

func TestApplyWritesRunsInsertsConcurrently(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	cal := &concurrentInsertCal{started: started, release: release}
	writes := []WriteOp{
		{Op: "insert", EventID: "e1"},
		{Op: "insert", EventID: "e2"},
		{Op: "insert", EventID: "e3"},
	}
	done := make(chan error, 1)
	go func() { done <- applyWrites(t.Context(), cal, writes) }()

	for range len(writes) {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("inserts did not run concurrently")
		}
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("applyWrites: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("applyWrites did not finish")
	}
}

// phaseCal applies writes to an in-memory calendar. Its delete is slow so an
// all-concurrent implementation would insert before the delete completed.
type phaseCal struct {
	stubCalendar
	mu     sync.Mutex
	events map[string]calendar.Event
	order  []string
	delay  time.Duration
}

func newPhaseCal(delay time.Duration) *phaseCal {
	return &phaseCal{events: map[string]calendar.Event{}, delay: delay}
}

func (c *phaseCal) Delete(_ context.Context, id string) error {
	time.Sleep(c.delay)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.events, id)
	c.order = append(c.order, "delete "+id)
	return nil
}

func (c *phaseCal) Insert(_ context.Context, e calendar.Event) (calendar.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.events[e.ID]; exists {
		return calendar.Event{}, &calendar.APIError{
			StatusCode: 409, Message: "The requested identifier already exists.",
			Path: "calendars/primary/events",
		}
	}
	c.events[e.ID] = e
	c.order = append(c.order, "insert "+e.ID)
	return e, nil
}

func (c *phaseCal) Patch(_ context.Context, id string, e calendar.Event) (calendar.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.ID = id
	c.events[id] = e
	c.order = append(c.order, "patch "+id)
	return e, nil
}

func (c *phaseCal) Calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.order...)
}

func (c *phaseCal) Has(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.events[id]
	return ok
}

func TestApplyWritesKeepsDeleteBeforeInsert(t *testing.T) {
	cal := newPhaseCal(20 * time.Millisecond)
	cal.events["same"] = calendar.Event{ID: "same"}

	// A corrupt ownership pointer makes diffPlan delete the event, while the
	// plan inserts its deterministic id. The phases must keep delete first:
	// an insert-first race would patch the event and the delete would then
	// remove it.
	if err := applyWrites(t.Context(), cal, []WriteOp{
		{Op: "delete", EventID: "same"},
		{Op: "insert", EventID: "same"},
	}); err != nil {
		t.Fatalf("applyWrites: %v", err)
	}
	if !cal.Has("same") {
		t.Fatal("delete and insert of the same event id left no event")
	}
	if diff := cmp.Diff([]string{"delete same", "insert same"}, cal.Calls()); diff != "" {
		t.Fatalf("call order mismatch (-want +got):\n%s", diff)
	}
}

type failingWriteCal struct {
	stubCalendar
	mu    sync.Mutex
	calls []string
	err   error
}

func (c *failingWriteCal) record(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *failingWriteCal) Delete(_ context.Context, id string) error {
	c.record("delete " + id)
	return c.err
}

func (c *failingWriteCal) Patch(_ context.Context, id string, e calendar.Event) (calendar.Event, error) {
	c.record("patch " + id)
	return e, nil
}

func (c *failingWriteCal) Insert(_ context.Context, e calendar.Event) (calendar.Event, error) {
	c.record("insert " + e.ID)
	return e, nil
}

func TestApplyWritesStopsAfterAFailedPhase(t *testing.T) {
	boom := errors.New("boom")
	cal := &failingWriteCal{err: boom}
	err := applyWrites(t.Context(), cal, []WriteOp{
		{Op: "delete", EventID: "e1"},
		{Op: "patch", EventID: "e2"},
		{Op: "insert", EventID: "e3"},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("applyWrites err = %v, want the delete failure", err)
	}
	cal.mu.Lock()
	defer cal.mu.Unlock()
	if got := strings.Join(cal.calls, ","); got != "delete e1" {
		t.Fatalf("calls = %q, want only the failed delete", got)
	}
}
