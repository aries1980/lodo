package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/grid"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

// at builds an instant in the fixture week: Monday 2026-11-09, like the
// run tests' week, safely after the October DST transition.
func at(day, hour, min int, loc *time.Location) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, loc)
}

// fakeAPI acts as the Calendar API serving fixtures: pages for each
// events.list call, events by id, or a Google error. Requests are recorded
// in order for query-shape asserts.
type fakeAPI struct {
	list  []string // events.list bodies, by page index
	byID  map[string]string
	query []url.Values // each list call's query, in order
	paths []string     // every path seen
	fail  bool
}

func (f *fakeAPI) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		if f.fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"your calendar is not shared","status":"PERMISSION_DENIED"}}`))
			return
		}
		switch {
		case strings.LastIndex(r.URL.Path, "/events/") >= 0:
			// events.get: everything after /events/ is the event id.
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/events/")+len("/events/"):]
			body, ok := f.byID[id]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":404,"message":"no such event","status":"notFound"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case strings.Contains(r.URL.Path, "/events"):
			f.query = append(f.query, r.URL.Query())
			page := 0
			if tok := r.URL.Query().Get("pageToken"); tok != "" {
				page, _ = strconv.Atoi(tok)
			}
			if page >= len(f.list) {
				t.Errorf("list page %d asked for beyond %d pages", page, len(f.list))
				http.Error(w, "out of pages", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.list[page]))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// listBody is one events.list body; next is the token of the page that
// follows, if any. Building pages eagerly keeps the fake's server loop
// free of state.
func listBody(items []string, next string) string {
	joined := strings.Join(items, ",")
	if next == "" {
		return fmt.Sprintf(`{"items": [%s]}`, joined)
	}
	return fmt.Sprintf(`{"items": [%s], "nextPageToken": %q}`, joined, next)
}

func TestListPaginatesAndEscapesTheCalendarID(t *testing.T) {
	loc := london(t)
	fa := &fakeAPI{
		list: []string{
			listBody([]string{timed("one", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")}, "1"),
			listBody([]string{timed("two", "2026-11-09T12:00:00Z", "2026-11-09T13:00:00Z")}, "2"),
			listBody([]string{timed("three", "2026-11-10T09:00:00Z", "2026-11-10T10:00:00Z")}, ""),
		},
	}
	c, err := NewClient(nil, "me@example.com", fa.serve(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	events, err := c.List(context.Background(), at(9, 8, 0, loc), at(11, 0, 0, loc))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, 0, len(events))
	for _, e := range events {
		got = append(got, e.ID)
	}
	if diff := cmp.Diff([]string{"one", "two", "three"}, got); diff != "" {
		t.Fatalf("List mismatch (-want +got):\n%s", diff)
	}

	// Three page requests, all against the same (URL-escaped) calendar.
	if len(fa.paths) != 3 || !strings.Contains(fa.paths[0], "/events") {
		t.Fatalf("paths = %v", fa.paths)
	}
	if got := fa.paths[0]; strings.Contains(got, "/%") && !strings.Contains(got, "example.com") {
		t.Errorf("the calendar id arrived escaped into yet another encoding: %q", got)
	}

	// The window and the ordering depart in the shape calendar.md asks
	// for: server-side orderBy=startTime under singleEvents, and one page
	// after another by its token.
	q := fa.query[0]
	if got := q.Get("timeMin"); got != "2026-11-09T08:00:00Z" {
		t.Errorf("timeMin = %q", got)
	}
	if got := q.Get("timeMax"); got != "2026-11-11T00:00:00Z" {
		t.Errorf("timeMax = %q", got)
	}
	if got := q.Get("singleEvents"); got != "true" {
		t.Errorf("singleEvents = %q", got)
	}
	if got := q.Get("orderBy"); got != "startTime" {
		t.Errorf("orderBy = %q", got)
	}
	if got := q.Get("maxResults"); got != "2500" {
		t.Errorf("maxResults = %q", got)
	}
	if tok := q.Get("pageToken"); tok != "" {
		t.Errorf("the first page must not carry a token: %q", tok)
	}
	if got := fa.query[1].Get("pageToken"); got != "1" {
		t.Errorf("second page token = %q, want 1", got)
	}
	if got := fa.query[2].Get("pageToken"); got != "2" {
		t.Errorf("third page token = %q, want 2", got)
	}
}

func TestGetByIDAndNotFound(t *testing.T) {
	fa := &fakeAPI{byID: map[string]string{
		"e-one": timed("e-one", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z"),
	}}
	c, err := NewClient(nil, "primary", fa.serve(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	e, err := c.Get(context.Background(), "e-one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.ID != "e-one" {
		t.Fatalf("Get id = %q", e.ID)
	}
	if _, err := c.Get(context.Background(), "absent"); err == nil ||
		!strings.Contains(err.Error(), "no such event") {
		t.Fatalf("Get(absent) = %v, want the 404's message", err)
	}
}

func TestAPIErrorSurfacesTheMessage(t *testing.T) {
	fa := &fakeAPI{fail: true}
	c, err := NewClient(nil, "primary", fa.serve(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.List(context.Background(), london32(t), london32(t).Add(time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "your calendar is not shared") ||
		!strings.Contains(err.Error(), "403") {
		t.Fatalf("List err = %v, want the calendar's own message", err)
	}
}

// london32 is a from time for the error-fake, which never serves pages.
func london32(t *testing.T) time.Time {
	return at(9, 8, 0, london(t))
}

// timed is a fixture event (confirmed plain meeting, RFC 3339 at its own
// offsets) whose extra fields ride along.
func timed(id, start, end string) string {
	return fmt.Sprintf(
		`{"id":%q,"status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q}}`, id, start, end)
}

// parseEvent decodes one fixture event body.
func parseEvent(t *testing.T, raw string) Event {
	t.Helper()
	var e Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return e
}

func TestClassifyTable(t *testing.T) {
	loc := london(t)
	start := at(9, 10, 0, loc).Format(time.RFC3339)
	end := at(9, 11, 0, loc).Format(time.RFC3339)

	tests := []struct {
		name  string
		extra string
		want  Kind
	}{
		{"a confirmed plain meeting is busy", "", KindBusy},
		{"cancelled is free", `,"status":"cancelled"`, KindFree},
		{"declined by the user is free",
			`,"attendees":[{"self":true,"responseStatus":"declined"}]`, KindFree},
		{"accepted by the user is busy",
			`,"attendees":[{"self":true,"responseStatus":"accepted"}]`, KindBusy},
		{"someone else's decline is still busy",
			`,"attendees":[{"self":false,"responseStatus":"declined"}]`, KindBusy},
		{"transparent is free", `,"transparency":"transparent"`, KindFree},
		{"tentative still busy", `,"status":"tentative"`, KindBusy},
		{"a recurrence marker does not decide", `,"recurrence":["RRULE:FREQ=WEEKLY"]`, KindBusy},
		{"lodo=true is managed", `,"extendedProperties":{"private":{"lodo":"true"}}`, KindManaged},
		{"lodo=true is managed even cancelled",
			`,"status":"cancelled","extendedProperties":{"private":{"lodo":"true"}}`, KindManaged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"id":%q,"start":{"dateTime":%q},"end":{"dateTime":%q}%s}`, "x", start, end, tt.extra)
			if got := Classify(parseEvent(t, raw)); got != tt.want {
				t.Fatalf("Classify = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSpanUsesItsOffsetsAndFallsBackToTheDate(t *testing.T) {
	loc := london(t)

	// A timed event keeps its own offsets: GMT here, DST elsewhere.
	start, end, err := parseEvent(t, timed("x", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")).Span(loc)
	if err != nil {
		t.Fatalf("Span: %v", err)
	}
	if !start.Equal(at(9, 10, 0, loc)) || !end.Equal(at(9, 11, 0, loc)) {
		t.Fatalf("span = %v..%v, want 10:00..11:00 local", start, end)
	}

	// A missing or malformed dateTime falls back to the date, read against
	// loc: the whole local day an all-day event occupies, whose end is the
	// day after the day Google states.
	start, end, err = parseEvent(t,
		`{"id":"all-day","start":{"dateTime":"garbage","date":"2026-11-09"},"end":{"date":"2026-11-10"}}`).Span(loc)
	if err != nil {
		t.Fatalf("Span: %v", err)
	}
	if !start.Equal(at(9, 0, 0, loc)) || !end.Equal(at(10, 0, 0, loc)) {
		t.Fatalf("span = %v..%v, want the whole Monday", start, end)
	}

	// Neither parseable, on the start: the error carries the event's id.
	if _, _, err := parseEvent(t,
		`{"id":"broken","start":{"dateTime":"garbage"},"end":{"date":"2026-11-10"}}`).Span(loc); err == nil ||
		!strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "start") {
		t.Fatalf("Span err = %v, want the event-id start complaint", err)
	}

	// An end in front of its start is corruption, refused into the open.
	if _, _, err := parseEvent(t, timed("backwards", "2026-11-09T11:00:00Z", "2026-11-09T10:00:00Z")).Span(loc); err == nil ||
		!strings.Contains(err.Error(), "not after it starts") {
		t.Fatalf("Span err = %v, want the end-before-start refusal", err)
	}
}

// busyFixture is the window the busy golden projects onto: Monday 2026-11-09
// and Tuesday, starting at 08:00 London.
func busyFixture(t *testing.T) (*grid.Grid, time.Time, time.Time) {
	t.Helper()
	loc := london(t)
	now := at(9, 8, 0, loc)
	return grid.Build(now, loc, 15*time.Minute, 2), now, at(11, 0, 0, loc)
}

func TestBusyProjectsTheGoldenMask(t *testing.T) {
	fa := &fakeAPI{
		list: []string{
			listBody([]string{
				// Sunday 23:00 to Monday 09:30: starts before the window,
				// clips at now.
				timed("earlier", "2026-11-08T23:00:00Z", "2026-11-09T09:30:00Z"),
				// A plain meeting, 10:00-12:00 Monday.
				timed("meeting", "2026-11-09T10:00:00Z", "2026-11-09T12:00:00Z"),
				// An all-day birthday: busy for the whole of Tuesday.
				`{"id":"birthday","status":"confirmed","start":{"date":"2026-11-10"},"end":{"date":"2026-11-11"}}`,
				// Crosses the window's end at Wednesday 00:00: clips.
				timed("late", "2026-11-10T22:00:00Z", "2026-11-11T01:00:00Z"),
				// A Managed event: no busy time in M4, returned instead.
				`{"id":"lodo-is-mine","summary":"A Block","status":"confirmed","start":{"dateTime":"2026-11-09T13:00:00Z"},"end":{"dateTime":"2026-11-09T14:00:00Z"},"extendedProperties":{"private":{"lodo":"true"}}}`,
				// The three kinds of nothing: cancelled, user-declined,
				// transparent (each at its own hour, for independent holes).
				`{"id":"cancelled","status":"cancelled","start":{"dateTime":"2026-11-09T15:00:00Z"},"end":{"dateTime":"2026-11-09T16:00:00Z"}}`,
				fmt.Sprintf(`{"id":"declined","status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q},"attendees":[{"self":true,"responseStatus":"declined"}]}`,
					at(9, 17, 0, london(t)).Format(time.RFC3339), at(9, 18, 0, london(t)).Format(time.RFC3339)),
				fmt.Sprintf(`{"id":"quiet","status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q},"transparency":"transparent"}`,
					at(9, 19, 0, london(t)).Format(time.RFC3339), at(9, 20, 0, london(t)).Format(time.RFC3339)),
			}, ""),
		},
	}
	g, now, _ := busyFixture(t)
	c, err := NewClient(nil, "primary", fa.serve(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	mask, managed, err := c.Busy(context.Background(), g, now)
	if err != nil {
		t.Fatalf("Busy: %v", err)
	}

	// The golden, slot by slot like the grid's own tests: the clipped
	// earlier-starter, the meeting, and the whole all-day Tuesday.
	want := g.Overlapping(at(9, 8, 0, london(t)), at(9, 9, 30, london(t))).
		Or(g.Overlapping(at(9, 10, 0, london(t)), at(9, 12, 0, london(t)))).
		Or(g.Overlapping(at(10, 0, 0, london(t)), at(11, 0, 0, london(t))))
	if diff := cmp.Diff(want, mask); diff != "" {
		t.Fatalf("busy mask mismatch (-want +got):\n%s", diff)
	}
	for i := 0; i < g.Len(); i++ {
		if mask.Has(i) != want.Has(i) {
			t.Fatalf("slot %d at %v: busy=%v want %v", i, g.Start(i).Format("15:04"), mask.Has(i), want.Has(i))
		}
	}

	// The window's events that block nothing leave daylight behind: the
	// Managed 13:00-14:00 (52..55), the cancelled 15:00-16:00 (60..63), the
	// declined 17:00-18:00 (76..79) and the transparent 19:00-20:00 (84..87)
	// are all holes in the mask.
	for _, i := range []int{52, 53, 54, 55, 60, 61, 62, 63, 76, 77, 78, 79, 84, 85, 86, 87} {
		if mask.Has(i) {
			t.Fatalf("slot %d at %v is busy, want a hole", i, g.Start(i).Format("Mon 15:04"))
		}
	}

	if len(managed) != 1 || managed[0].ID != "lodo-is-mine" || managed[0].Summary != "A Block" {
		t.Fatalf("managed events = %+v, want the one the calendar named", managed)
	}
}

func TestBusyRefusesAnUntruthfulCalendar(t *testing.T) {
	fa := &fakeAPI{list: []string{
		listBody([]string{timed("broken", "2026-11-09T10:00:00Z", "garbage")}, ""),
	}}
	g, now, _ := busyFixture(t)
	c, err := NewClient(nil, "primary", fa.serve(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, _, err := c.Busy(context.Background(), g, now); err == nil ||
		!strings.Contains(err.Error(), "broken") {
		t.Fatalf("Busy = %v, want a time-parse refusal, not a silent hole", err)
	}
}
