package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/grid"
)

// multiFake serves per-calendar events.list bodies or per-calendar HTTP
// failures, routing by the calendar ID in the path. Requests are recorded
// for order asserts; reads are concurrent, so recording is guarded.
type multiFake struct {
	bodies map[string][]string      // calendar ID -> list bodies by page
	status map[string]int           // calendar ID -> HTTP status to fail with
	delay  map[string]time.Duration // calendar ID -> response delay

	mu    sync.Mutex
	paths []string
}

// Paths returns the recorded request paths in arrival order.
func (f *multiFake) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func (f *multiFake) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		id := calendarIDFromPath(r.URL.Path)
		if d := f.delay[id]; d > 0 {
			time.Sleep(d)
		}
		if code, ok := f.status[id]; ok && code != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			if code == http.StatusForbidden {
				_, _ = w.Write([]byte(`{"error":{"code":403,"message":"forbidden"}}`))
			} else {
				_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
			}
			return
		}
		bodies := f.bodies[id]
		if len(bodies) == 0 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items": []}`))
			return
		}
		page := 0
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			// Pages are indexed by token in these fixtures.
			for i := range bodies {
				if strings.Contains(bodies[i], tok) {
					page = i
					break
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[page]))
	}))
	srv.Start()
	return srv.URL
}

// calendarIDFromPath extracts the calendar ID from
// /calendars/<id>/events, unescaping it.
func calendarIDFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if part == "calendars" && i+1 < len(parts) {
			id, _ := url.PathUnescape(parts[i+1])
			return id
		}
	}
	return ""
}

func TestMultiUnion(t *testing.T) {
	loc := london(t)
	fa := &multiFake{bodies: map[string][]string{
		"primary": {listBody([]string{timed("w-meet", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
		"a@example.com": {listBody([]string{
			timed("a-meet", "2026-11-09T11:00:00Z", "2026-11-09T12:00:00Z"),
		}, "")},
		"b@example.com": {listBody([]string{
			timed("b-meet", "2026-11-09T14:00:00Z", "2026-11-09T15:00:00Z"),
		}, "")},
	}}
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com", "b@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	mask, managed, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	if len(managed) != 0 {
		t.Fatalf("managed = %+v, want none", managed)
	}
	want := g.Overlapping(at(9, 10, 0, loc), at(9, 11, 0, loc)).
		Or(g.Overlapping(at(9, 11, 0, loc), at(9, 12, 0, loc))).
		Or(g.Overlapping(at(9, 14, 0, loc), at(9, 15, 0, loc)))
	for i := 0; i < g.Len(); i++ {
		if mask.Has(i) != want.Has(i) {
			t.Fatalf("slot %d: busy=%v want %v", i, mask.Has(i), want.Has(i))
		}
	}
	if report.WriteCalendar != "primary" || report.WriteEvents != 1 {
		t.Fatalf("report write = %+v", report)
	}
	if len(report.Sources) != 2 || report.Sources[0].Events != 1 || report.Sources[1].Events != 1 {
		t.Fatalf("sources = %+v", report.Sources)
	}
	// The write calendar is read first; the read calendars fan out, so only
	// the audit rows promise config order.
	paths := fa.Paths()
	if len(paths) != 3 {
		t.Fatalf("paths = %v, want write plus two reads", paths)
	}
	if got := calendarIDFromPath(paths[0]); got != "primary" {
		t.Fatalf("first read = %q, want the write calendar", got)
	}
	reads := []string{calendarIDFromPath(paths[1]), calendarIDFromPath(paths[2])}
	slices.Sort(reads)
	if want := []string{"a@example.com", "b@example.com"}; !slices.Equal(reads, want) {
		t.Fatalf("read calendars = %v, want %v", reads, want)
	}
}

func TestMultiDedupesWriteID(t *testing.T) {
	fa := &multiFake{bodies: map[string][]string{
		"primary": {listBody([]string{timed("w", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
	}}
	m, err := NewMultiClient(nil, "primary", []string{"primary", " primary ", "", "a@example.com", "a@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	if diff := len(m.ReadIDs()); diff != 1 {
		t.Fatalf("ReadIDs = %q, want only a@example.com", m.ReadIDs())
	}
	g, now, _ := busyFixture(t)
	_, _, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	if len(report.Sources) != 1 || report.Sources[0].CalendarID != "a@example.com" {
		t.Fatalf("sources = %+v", report.Sources)
	}
}

func TestLodoOnReadIsBusyWithWarning(t *testing.T) {
	loc := london(t)
	fa := &multiFake{bodies: map[string][]string{
		"primary": {listBody(nil, "")},
		"a@example.com": {listBody([]string{
			`{"id":"copy","status":"confirmed","start":{"dateTime":"2026-11-09T10:00:00Z"},"end":{"dateTime":"2026-11-09T11:00:00Z"},"extendedProperties":{"private":{"lodo":"true","task":"t","block":"0"}}}`,
		}, "")},
	}}
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	mask, managed, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	if len(managed) != 0 {
		t.Fatalf("managed = %+v, read lodo=true must never be adopted", managed)
	}
	want := g.Overlapping(at(9, 10, 0, loc), at(9, 11, 0, loc))
	for i := 0; i < g.Len(); i++ {
		if mask.Has(i) != want.Has(i) {
			t.Fatalf("slot %d: busy=%v want %v", i, mask.Has(i), want.Has(i))
		}
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "copy") || !strings.Contains(report.Warnings[0], "lodo=true") {
		t.Fatalf("warnings = %+v, want the lodo-on-read warning naming the event", report.Warnings)
	}
}

func TestMulti403WarnsAndContinues(t *testing.T) {
	loc := london(t)
	fa := &multiFake{
		bodies: map[string][]string{
			"primary":          {listBody([]string{timed("w", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
			"good@example.com": {listBody([]string{timed("g", "2026-11-09T14:00:00Z", "2026-11-09T15:00:00Z")}, "")},
		},
		status: map[string]int{"bad@example.com": http.StatusForbidden},
	}
	m, err := NewMultiClient(nil, "primary", []string{"bad@example.com", "good@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	mask, _, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v, 403 must warn and continue", err)
	}
	want := g.Overlapping(at(9, 10, 0, loc), at(9, 11, 0, loc)).
		Or(g.Overlapping(at(9, 14, 0, loc), at(9, 15, 0, loc)))
	for i := 0; i < g.Len(); i++ {
		if mask.Has(i) != want.Has(i) {
			t.Fatalf("slot %d: busy=%v want %v", i, mask.Has(i), want.Has(i))
		}
	}
	if len(report.Sources) != 2 || report.Sources[0].Status != SourceSkipped403 || report.Sources[1].Status != SourceRead {
		t.Fatalf("sources = %+v", report.Sources)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "bad@example.com") ||
		!strings.Contains(report.Warnings[0], "403") || !strings.Contains(report.Warnings[0], "free/busy-only") {
		t.Fatalf("warnings = %+v, want the 403 warning naming the calendar with free/busy-only", report.Warnings)
	}
}

func TestMulti404WarnsAndContinues(t *testing.T) {
	fa := &multiFake{
		bodies: map[string][]string{
			"primary": {listBody([]string{timed("w", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
		},
		status: map[string]int{"gone@example.com": http.StatusNotFound},
	}
	m, err := NewMultiClient(nil, "primary", []string{"gone@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	if _, _, report, err := m.Busy(t.Context(), g, now); err != nil {
		t.Fatalf("Busy: %v, 404 must warn and continue", err)
	} else if len(report.Sources) != 1 || report.Sources[0].Status != SourceSkipped404 {
		t.Fatalf("sources = %+v", report.Sources)
	} else if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "gone@example.com") ||
		!strings.Contains(report.Warnings[0], "404") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestMultiAuthFailureFailsTheRun(t *testing.T) {
	fa := &multiFake{
		bodies: map[string][]string{
			"primary": {listBody([]string{timed("w", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
		},
		status: map[string]int{"bad@example.com": http.StatusInternalServerError},
	}
	m, err := NewMultiClient(nil, "primary", []string{"bad@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	if _, _, _, err := m.Busy(t.Context(), g, now); err == nil {
		t.Fatal("Busy succeeded, want the 500 to fail the run")
	}
}

func TestWriteFailureFailsTheRun(t *testing.T) {
	fa := &multiFake{status: map[string]int{"primary": http.StatusForbidden}}
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	if _, _, _, err := m.Busy(t.Context(), g, now); err == nil {
		t.Fatal("Busy succeeded, want the write 403 to fail the run")
	}
}

func TestWrite401HintsReauth(t *testing.T) {
	fa := &multiFake{status: map[string]int{"primary": http.StatusUnauthorized}}
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	_, _, _, err = m.Busy(t.Context(), g, now)
	if err == nil {
		t.Fatal("Busy succeeded, want the write 401 to fail the run")
	}
	msg := err.Error()
	for _, want := range []string{`write calendar "primary"`, "401", "lodo auth google"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Busy err = %q, want it to contain %q", msg, want)
		}
	}
}

func TestRead401FailsWithCalendarName(t *testing.T) {
	fa := &multiFake{
		bodies: map[string][]string{
			"primary": {listBody([]string{timed("w", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "")},
		},
		status: map[string]int{"bad@example.com": http.StatusUnauthorized},
	}
	m, err := NewMultiClient(nil, "primary", []string{"bad@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	_, _, _, err = m.Busy(t.Context(), g, now)
	if err == nil {
		t.Fatal("Busy succeeded, want the read 401 to fail the run (only 403/404 warn)")
	}
	msg := err.Error()
	for _, want := range []string{`read calendar "bad@example.com"`, "401", "lodo auth google"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Busy err = %q, want it to contain %q", msg, want)
		}
	}
}

func TestWriteTransportInvalidGrantHintsReauth(t *testing.T) {
	hc := &http.Client{Transport: authFailTransport{code: "invalid_grant", desc: "Bad Request"}}
	m, err := NewMultiClient(hc, "primary", []string{"a@example.com"}, "https://www.example.com", ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	_, _, _, err = m.Busy(t.Context(), g, now)
	if err == nil {
		t.Fatal("Busy succeeded, want the revoked refresh to fail")
	}
	msg := err.Error()
	for _, want := range []string{`write calendar "primary"`, "invalid_grant", "lodo auth google"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Busy err = %q, want it to contain %q", msg, want)
		}
	}
}

func TestReadRulesMatchWrite(t *testing.T) {
	loc := london(t)
	// All-day on a read calendar blocks the whole Tuesday; cancelled,
	// declined and transparent leave holes; a plain meeting blocks.
	fa := &multiFake{bodies: map[string][]string{
		"primary": {listBody(nil, "")},
		"a@example.com": {listBody([]string{
			`{"id":"bday","status":"confirmed","start":{"date":"2026-11-10"},"end":{"date":"2026-11-11"}}`,
			timed("meet", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z"),
			`{"id":"cancelled","status":"cancelled","start":{"dateTime":"2026-11-09T15:00:00Z"},"end":{"dateTime":"2026-11-09T16:00:00Z"}}`,
			`{"id":"declined","status":"confirmed","start":{"dateTime":"2026-11-09T17:00:00+00:00"},"end":{"dateTime":"2026-11-09T18:00:00+00:00"},"attendees":[{"self":true,"responseStatus":"declined"}]}`,
			`{"id":"quiet","status":"confirmed","start":{"dateTime":"2026-11-09T19:00:00+00:00"},"end":{"dateTime":"2026-11-09T20:00:00+00:00"},"transparency":"transparent"}`,
		}, "")},
	}}
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	mask, _, _, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	want := g.Overlapping(at(9, 10, 0, loc), at(9, 11, 0, loc)).
		Or(g.Overlapping(at(10, 0, 0, loc), at(11, 0, 0, loc)))
	for i := 0; i < g.Len(); i++ {
		if mask.Has(i) != want.Has(i) {
			t.Fatalf("slot %d: busy=%v want %v", i, mask.Has(i), want.Has(i))
		}
	}
}

func TestReadAllDayPolicy(t *testing.T) {
	loc := london(t)
	// Tuesday birthday, Monday holiday, and a timed Monday meeting whose
	// title contains "birthday". The days differ so a skipped all-day event
	// leaves a visible hole.
	birthday := `{"id":"bday","summary":"Alice's birthday","status":"confirmed","start":{"date":"2026-11-10"},"end":{"date":"2026-11-11"}}`
	holiday := `{"id":"hol","summary":"Bank holiday","status":"confirmed","start":{"date":"2026-11-09"},"end":{"date":"2026-11-10"}}`
	lunch := strings.Replace(
		timed("lunch", "2026-11-10T10:00:00Z", "2026-11-10T11:00:00Z"),
		`"id":"lunch"`, `"id":"lunch","summary":"Birthday lunch"`, 1)

	t.Run("default keeps the read all-day event busy", func(t *testing.T) {
		fa := &multiFake{bodies: map[string][]string{
			"primary":       {listBody(nil, "")},
			"a@example.com": {listBody([]string{birthday}, "")},
		}}
		g, mask := readMask(t, fa, ReadOptions{})
		want := g.Overlapping(at(10, 0, 0, loc), at(11, 0, 0, loc))
		assertMask(t, g, mask, want)
	})

	t.Run("ignore all-day frees every read all-day event", func(t *testing.T) {
		fa := &multiFake{bodies: map[string][]string{
			"primary":       {listBody([]string{birthday}, "")},
			"a@example.com": {listBody([]string{birthday, holiday, lunch}, "")},
		}}
		g, mask := readMask(t, fa, ReadOptions{IgnoreAllDay: true})
		// Write-calendar birthday still blocks Tuesday. The read holiday
		// (Monday) and birthday do not, so Monday stays free. The timed
		// lunch sits inside that Tuesday and does not widen the mask.
		want := g.Overlapping(at(10, 0, 0, loc), at(11, 0, 0, loc))
		assertMask(t, g, mask, want)
	})

	t.Run("a phrase frees only matching all-day events", func(t *testing.T) {
		fa := &multiFake{bodies: map[string][]string{
			"primary":       {listBody(nil, "")},
			"a@example.com": {listBody([]string{birthday, holiday, lunch}, "")},
		}}
		g, mask := readMask(t, fa, ReadOptions{IgnoreAllDayMatching: []string{" Birthday "}})
		// Monday holiday stays. Tuesday birthday is skipped. The timed
		// "Birthday lunch" on Tuesday stays.
		want := g.Overlapping(at(9, 8, 0, loc), at(10, 0, 0, loc)).
			Or(g.Overlapping(at(10, 10, 0, loc), at(10, 11, 0, loc)))
		assertMask(t, g, mask, want)
	})
}

func readMask(t *testing.T, fa *multiFake, opt ReadOptions) (*grid.Grid, grid.Mask) {
	t.Helper()
	m, err := NewMultiClient(nil, "primary", []string{"a@example.com"}, fa.serve(t), opt)
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	mask, _, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	if len(report.Sources) == 0 || report.Sources[0].Events == 0 {
		t.Fatal("skipped events must still count in the audit total")
	}
	return g, mask
}

func assertMask(t *testing.T, g *grid.Grid, got, want grid.Mask) {
	t.Helper()
	for i := 0; i < g.Len(); i++ {
		if got.Has(i) != want.Has(i) {
			t.Fatalf("slot %d at %v: busy=%v want %v", i, g.Start(i).Format("Mon 15:04"), got.Has(i), want.Has(i))
		}
	}
}

// sequentialBusy is the pre-fan-out Busy loop, kept as the oracle for
// TestMultiBusyMatchesSequentialRead. Do not simplify it to call Busy.
func sequentialBusy(ctx context.Context, m *MultiClient, g *grid.Grid, now time.Time) (grid.Mask, []Event, BusyReport, error) {
	report := BusyReport{WriteCalendar: m.writeID, Shared: grid.NewMask(g.Len())}
	union := grid.NewMask(g.Len())

	writeEvents, managed, err := m.write.listBusy(ctx, g, now)
	if err != nil {
		return nil, nil, BusyReport{}, err
	}
	report.WriteEvents = len(writeEvents)
	writeBusy, err := busyMask(g, writeEvents, now)
	if err != nil {
		return nil, nil, BusyReport{}, err
	}
	union.OrInto(writeBusy)

	for i, rc := range m.reads {
		id := m.readIDs[i]
		events, err := rc.List(ctx, now, g.WindowEnd())
		if err != nil {
			if code, ok := StatusCodeOf(err); ok {
				switch code {
				case http.StatusForbidden:
					report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceSkipped403})
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("read calendar %q skipped (403): access denied (revoked, never accepted, or free/busy-only share; free/busy-only shares are not yet supported)", id))
					continue
				case http.StatusNotFound:
					report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceSkipped404})
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("read calendar %q skipped (404): not found (removed calendar)", id))
					continue
				}
			}
			return nil, nil, BusyReport{}, err
		}
		report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceRead, Events: len(events)})
		for _, e := range events {
			if e.Managed() {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("read calendar %q: event %q carries lodo=true, treated as ordinary busy time (never adopted, never moved)", id, e.ID))
			} else if Classify(e) == KindFree {
				continue
			}
			if m.skipReadAllDay(e) {
				continue
			}
			start, end, err := projectSpan(e, g, now)
			if err != nil {
				return nil, nil, BusyReport{}, err
			}
			if start.IsZero() {
				continue
			}
			slot := g.Overlapping(start, end)
			union.OrInto(slot)
			report.Shared.OrInto(slot)
		}
	}
	return union, managed, report, nil
}

func lodoReadEvent(id, start, end string) string {
	return fmt.Sprintf(`{"id":%q,"status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q},"extendedProperties":{"private":{"lodo":"true","task":"t","block":"0"}}}`, id, start, end)
}

func TestMultiBusyMatchesSequentialRead(t *testing.T) {
	fa := &multiFake{
		bodies: map[string][]string{
			"primary": {listBody([]string{
				timed("w-meet", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z"),
			}, "")},
			"ok@example.com": {listBody([]string{
				timed("ok-meet", "2026-11-09T14:00:00Z", "2026-11-09T15:00:00Z"),
				`{"id":"ok-bday","status":"confirmed","start":{"date":"2026-11-10"},"end":{"date":"2026-11-11"}}`,
			}, "")},
			"copy@example.com": {listBody([]string{
				lodoReadEvent("copied", "2026-11-09T16:00:00Z", "2026-11-09T17:00:00Z"),
				timed("copy-meet", "2026-11-09T18:00:00Z", "2026-11-09T19:00:00Z"),
			}, "")},
		},
		status: map[string]int{
			"bad@example.com":  http.StatusForbidden,
			"gone@example.com": http.StatusNotFound,
		},
	}
	m, err := NewMultiClient(nil, "primary",
		[]string{"ok@example.com", "bad@example.com", "gone@example.com", "copy@example.com"},
		fa.serve(t), ReadOptions{IgnoreAllDay: true})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)

	wantMask, wantManaged, wantReport, wantErr := sequentialBusy(t.Context(), m, g, now)
	gotMask, gotManaged, gotReport, gotErr := m.Busy(t.Context(), g, now)

	if (wantErr != nil) != (gotErr != nil) {
		t.Fatalf("errors = %v, want %v", gotErr, wantErr)
	}
	if wantErr != nil && wantErr.Error() != gotErr.Error() {
		t.Fatalf("error = %q, want %q", gotErr, wantErr)
	}
	if diff := cmp.Diff(wantMask, gotMask); diff != "" {
		t.Fatalf("mask mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantManaged, gotManaged); diff != "" {
		t.Fatalf("managed mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantReport, gotReport); diff != "" {
		t.Fatalf("report mismatch (-want +got):\n%s", diff)
	}
}

func TestMultiReadsCalendarsConcurrently(t *testing.T) {
	arrived := make(chan string, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := calendarIDFromPath(r.URL.Path)
		if id != "primary" {
			arrived <- id
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	srv.Start()

	m, err := NewMultiClient(nil, "primary", []string{"a@example.com", "b@example.com"}, srv.URL, ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)

	done := make(chan struct{})
	var (
		report BusyReport
		gotErr error
	)
	go func() {
		_, _, report, gotErr = m.Busy(t.Context(), g, now)
		close(done)
	}()

	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("read calendars did not run concurrently")
		}
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Busy did not finish after releasing the reads")
	}
	if gotErr != nil {
		t.Fatalf("Busy: %v", gotErr)
	}
	if len(report.Sources) != 2 ||
		report.Sources[0].CalendarID != "a@example.com" ||
		report.Sources[1].CalendarID != "b@example.com" {
		t.Fatalf("sources = %+v, want config order", report.Sources)
	}
}

func TestMultiKeepsConfigOrderWhenReadsFinishOutOfOrder(t *testing.T) {
	fa := &multiFake{
		bodies: map[string][]string{
			"primary": {listBody(nil, "")},
			"slow@example.com": {listBody([]string{
				lodoReadEvent("slow-copy", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z"),
			}, "")},
			"fast@example.com": {listBody([]string{
				lodoReadEvent("fast-copy", "2026-11-09T14:00:00Z", "2026-11-09T15:00:00Z"),
			}, "")},
		},
		delay: map[string]time.Duration{"slow@example.com": 30 * time.Millisecond},
	}
	m, err := NewMultiClient(nil, "primary", []string{"slow@example.com", "fast@example.com"}, fa.serve(t), ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)

	_, _, report, err := m.Busy(t.Context(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}
	if len(report.Sources) != 2 ||
		report.Sources[0].CalendarID != "slow@example.com" ||
		report.Sources[1].CalendarID != "fast@example.com" {
		t.Fatalf("sources = %+v, want config order", report.Sources)
	}
	if len(report.Warnings) != 2 ||
		!strings.Contains(report.Warnings[0], "slow-copy") ||
		!strings.Contains(report.Warnings[1], "fast-copy") {
		t.Fatalf("warnings = %+v, want config order", report.Warnings)
	}
}

func TestMultiFirstHardErrorWinsOverCancellation(t *testing.T) {
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch id := calendarIDFromPath(r.URL.Path); id {
		case "primary":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "slow@example.com":
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
		}
	}))
	srv.Start()

	m, err := NewMultiClient(nil, "primary", []string{"slow@example.com", "bad@example.com"}, srv.URL, ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)

	_, _, _, err = m.Busy(t.Context(), g, now)
	if err == nil {
		t.Fatal("Busy succeeded, want the failing read to fail the run")
	}
	msg := err.Error()
	if !strings.Contains(msg, `read calendar "bad@example.com"`) || !strings.Contains(msg, "HTTP 500") {
		t.Fatalf("Busy err = %q, want the read calendar's 500", msg)
	}
	if strings.Contains(msg, "context canceled") {
		t.Fatalf("Busy err = %q, want the real failure, not the cancellation", msg)
	}
}

func TestMultiHonorsCallerCancellation(t *testing.T) {
	arrived := make(chan string, 2)
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calendarIDFromPath(r.URL.Path) != "primary" {
			arrived <- calendarIDFromPath(r.URL.Path)
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	srv.Start()

	m, err := NewMultiClient(nil, "primary", []string{"a@example.com", "b@example.com"}, srv.URL, ReadOptions{})
	if err != nil {
		t.Fatalf("NewMultiClient: %v", err)
	}
	g, now, _ := busyFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, _, err := m.Busy(ctx, g, now)
		done <- err
	}()

	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("read calendars did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Busy err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Busy did not stop after cancellation")
	}
}
