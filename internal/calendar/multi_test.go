package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/aries1980/lodo/internal/grid"
)

// multiFake serves per-calendar events.list bodies or per-calendar HTTP
// failures, routing by the calendar ID in the path. Requests are recorded
// in order for determinism asserts.
type multiFake struct {
	bodies map[string][]string // calendar ID -> list bodies by page
	status map[string]int      // calendar ID -> HTTP status to fail with
	paths  []string
}

func (f *multiFake) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		id := calendarIDFromPath(r.URL.Path)
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
	t.Cleanup(srv.Close)
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
	mask, managed, report, err := m.Busy(context.Background(), g, now)
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
	// Reads ran sequentially in config order.
	if len(fa.paths) != 3 {
		t.Fatalf("paths = %v, want write plus two reads", fa.paths)
	}
	if got := calendarIDFromPath(fa.paths[1]); got != "a@example.com" {
		t.Fatalf("second read = %q, want a@example.com", got)
	}
	if got := calendarIDFromPath(fa.paths[2]); got != "b@example.com" {
		t.Fatalf("third read = %q, want b@example.com", got)
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
	_, _, report, err := m.Busy(context.Background(), g, now)
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
	mask, managed, report, err := m.Busy(context.Background(), g, now)
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
	mask, _, report, err := m.Busy(context.Background(), g, now)
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
	if _, _, report, err := m.Busy(context.Background(), g, now); err != nil {
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
	if _, _, _, err := m.Busy(context.Background(), g, now); err == nil {
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
	if _, _, _, err := m.Busy(context.Background(), g, now); err == nil {
		t.Fatal("Busy succeeded, want the write 403 to fail the run")
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
	mask, _, _, err := m.Busy(context.Background(), g, now)
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
	mask, _, report, err := m.Busy(context.Background(), g, now)
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
