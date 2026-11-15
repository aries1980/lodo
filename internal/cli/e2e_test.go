package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/logseq"
)

// e2eLondon is the scheduling timezone every end-to-end fixture pins. A
// missing zone database is a broken build, not a reason to drift to UTC.
var e2eLondon = func() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic("e2e: Europe/London: " + err.Error())
	}
	return loc
}()

// e2eNow is the instant every end-to-end fixture derives from: a Wednesday
// mid-morning, so the week's windows are still ahead and a 7-day window ends
// on the following Tuesday with one Monday inside it.
var e2eNow = time.Date(2026, time.November, 11, 10, 0, 0, 0, e2eLondon)

// e2eDay names the date days after e2eNow.
func e2eDay(days int) string {
	return e2eNow.AddDate(0, 0, days).Format(time.DateOnly)
}

// e2eAt is the wall-clock instant days after e2eNow in the fixture zone.
func e2eAt(days, hour, min int) time.Time {
	y, m, d := e2eNow.AddDate(0, 0, days).Date()
	return time.Date(y, m, d, hour, min, 0, 0, e2eLondon)
}

// e2eFutureWeekday names the first weekday after e2eNow, so a plan-end
// derived from it is always in the future.
func e2eFutureWeekday(w time.Weekday) string {
	d := e2eNow
	for range 7 {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() == w {
			return d.Format(time.DateOnly)
		}
	}
	panic("e2e: no future weekday found")
}

// e2ePin renders a Logseq scheduled property relative to e2eNow.
func e2ePin(days, hour, min int) string {
	return e2eAt(days, hour, min).Format("<2006-01-02 Mon 15:04>")
}

// The credentials files every fixture writes.
const (
	e2eLogseqCreds = `{"logseq":{"token":"st-token"}}`
	e2eGoogleCreds = `{"logseq":{"token":"st-token"},"google":{"client_id":"cid","client_secret":"secret","refresh_token":"1//stored-refresh-token"}}`
)

// calendarEvent builds one Calendar v3 event body. managed marks it as
// Lodo's own, which the busy mask leaves out until schedule folds it in.
func calendarEvent(id, summary string, start, end time.Time, managed bool) map[string]any {
	event := map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"start": map[string]any{"dateTime": start.Format(time.RFC3339)},
		"end":   map[string]any{"dateTime": end.Format(time.RFC3339)},
	}
	if managed {
		event["extendedProperties"] = map[string]any{"private": map[string]any{"lodo": "true"}}
	}
	return event
}

func TestE2EAnchorsAreStable(t *testing.T) {
	// The fixture anchor is load-bearing: these assertions fail loudly if
	// someone moves it without re-checking the weekday-dependent scenarios.
	if got := e2eNow.Weekday(); got != time.Wednesday {
		t.Fatalf("e2eNow is a %s, want a Wednesday", got)
	}
	if got := e2eFutureWeekday(time.Friday); got != "2026-11-13" {
		t.Fatalf("next Friday = %q, want 2026-11-13", got)
	}
	if got := e2eDay(-1); got != "2026-11-10" {
		t.Fatalf("yesterday = %q, want 2026-11-10", got)
	}
	if got := e2eDay(30); got != "2026-12-11" {
		t.Fatalf("+30 days = %q, want 2026-12-11", got)
	}
}

// e2eOptions is one fixture's graph, calendar, and credential state. The zero
// value is the standard read-only fixture: one 30m TODO in a default
// Mon-Fri 09:00-12:00 map, a live Logseq stub, and no Google grant.
type e2eOptions struct {
	Tasks []*logseq.Block
	Maps  []*logseq.Block

	// Google adds a stored refresh token to the credentials file.
	Google bool
	// CalendarEvents seeds the fake Calendar before the run.
	CalendarEvents []map[string]any
	// CalendarReads overrides the events.list body per calendar ID.
	CalendarReads map[string]string
	// CalendarStatus fails events.list per calendar ID with this status.
	CalendarStatus map[string]int
	// ReadCalendars writes calendar.read_calendars.
	ReadCalendars []string
	// ConfigExtra is appended to config.yaml (ui.theme, ...).
	ConfigExtra string
	// Now pins the scheduling clock; the zero value uses e2eNow.
	Now time.Time
}

// e2e is one isolated Lodo world: a Logseq stub, a Google stub, config and
// credentials under their own XDG roots, and a pinned clock.
type e2e struct {
	Logseq   *logseqFake
	Calendar *fakeCalendar
	Dir      string
	Conf     string
	Cred     string
}

// newE2E builds a fixture. Callers that need to report setup failures should
// use mustE2E; godog worlds propagate the error to the scenario.
func newE2E(t testing.TB, o e2eOptions) (*e2e, error) {
	t.Helper()
	if o.Tasks == nil {
		o.Tasks = e2eTasks()
	}
	if o.Maps == nil {
		o.Maps = e2eMaps()
	}
	now := o.Now
	if now.IsZero() {
		now = e2eNow
	}

	dir := t.TempDir()
	api := newLogseqFake(t, o.Tasks, o.Maps)

	cal := newFakeCalendar()
	for _, ev := range o.CalendarEvents {
		cal.AddEvent(ev)
	}
	for id, body := range o.CalendarReads {
		cal.SetRead(id, body)
	}
	for id, status := range o.CalendarStatus {
		cal.SetStatus(id, status)
	}
	cal.Mount(t)

	conf, err := writeConfig(t, dir, api.APIURL(), calendarExtra(o))
	if err != nil {
		return nil, err
	}
	credBody := e2eLogseqCreds
	if o.Google {
		credBody = e2eGoogleCreds
	}
	cred, err := writeCredentials(t, dir, credBody)
	if err != nil {
		return nil, err
	}
	t.Setenv("LODO_NOW", now.Format(time.RFC3339))

	e := &e2e{Logseq: api, Calendar: cal, Dir: dir, Conf: conf, Cred: cred}
	return e, nil
}

func mustE2E(t *testing.T, o e2eOptions) *e2e {
	t.Helper()
	e, err := newE2E(t, o)
	if err != nil {
		t.Fatalf("e2e fixture: %v", err)
	}
	return e
}

// Close tears the fixture down early. NewTestServer owns the t cleanup, but a
// godog suite runs every scenario on one *testing.T, so reset() must release
// this scenario's listeners before the next one repoints the endpoints.
func (e *e2e) Close() {
	e.Logseq.Close()
	e.Calendar.Close()
}

// Run executes lodo against the fixture and returns its exit code and both
// streams.
func (e *e2e) Run(args ...string) (code int, stdout, stderr string) {
	stdoutBuf, stderrBuf := &bytes.Buffer{}, &strings.Builder{}
	code = Run(append([]string{"--config", e.Conf}, args...), stdoutBuf, stderrBuf)
	return code, stdoutBuf.String(), stderrBuf.String()
}

// runCLI runs lodo with the given config path. Fixtures own the XDG roots;
// callers that build their own paths set them with writeConfig and
// writeCredentials first.
func runCLI(t *testing.T, conf string, args ...string) (int, string, string) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run(append([]string{"--config", conf}, args...), stdout, stderr)
	return code, stdout.String(), stderr.String()
}

// e2eTasks is the standard graph: one 30m TODO that fits the default map.
func e2eTasks() []*logseq.Block {
	return []*logseq.Block{{
		UUID:       "t-task-uuid",
		Content:    "TODO Write the milestone note",
		Marker:     "TODO",
		Properties: map[string]any{"duration": "30m"},
	}}
}

// e2eMaps is the standard default map: weekday mornings in the window.
func e2eMaps() []*logseq.Block {
	return []*logseq.Block{{
		UUID:       "map-deep-uuid",
		Content:    "Deep Work",
		Properties: map[string]any{"default": true},
		Children:   []*logseq.Block{{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"}},
	}}
}

func calendarExtra(o e2eOptions) string {
	extra := ""
	if len(o.ReadCalendars) > 0 {
		extra += "  read_calendars:\n"
		for _, id := range o.ReadCalendars {
			extra += "    - " + id + "\n"
		}
	}
	return extra + o.ConfigExtra
}

// writeConfig writes config.yaml under dir and points XDG_CONFIG_HOME at it.
// extra is appended to the file, so it can extend the calendar or ui sections.
func writeConfig(t testing.TB, dir, apiURL, extra string) (string, error) {
	t.Helper()
	conf := filepath.Join(dir, "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o700); err != nil {
		return "", err
	}
	body := fmt.Sprintf(`
logseq:
  api_url: %s/api
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 7
  timezone: Europe/London
calendar:
  provider: google
  id: primary
%s
`, apiURL, extra)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return "", err
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return conf, nil
}

// writeCredentials writes credentials.json under dir and points the XDG state
// and home roots at it.
func writeCredentials(t testing.TB, dir, body string) (string, error) {
	t.Helper()
	cred := filepath.Join(dir, "lodo", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(cred, []byte(body), 0o600); err != nil {
		return "", err
	}
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	return cred, nil
}

// pointGoogle overlays the CLI's Google endpoints onto a fake and restores
// them when the test leaves.
func pointGoogle(t testing.TB, endpoint oauth2.Endpoint, base string) {
	t.Helper()
	oldEndpoint, oldBase := googleEndpoint, googleCalendarBase
	googleEndpoint = endpoint
	googleCalendarBase = base
	t.Cleanup(func() { googleEndpoint, googleCalendarBase = oldEndpoint, oldBase })
}

// logseqFake is the Logseq HTTP API the read side and the TUI use: one
// journal page, the Time Maps page, and update_block writes recorded in call
// order.
type logseqFake struct {
	mu     sync.Mutex
	writes []string
	srv    *httptest.Server
}

func newLogseqFake(t testing.TB, tasks, maps []*logseq.Block) *logseqFake {
	t.Helper()
	f := &logseqFake{}
	taskRaw, _ := json.Marshal(tasks)
	mapRaw, _ := json.Marshal(maps)
	// NewTestServer owns the shutdown; Start() keeps a loopback URL, because
	// the CLI builds its own client from the config that carries it.
	f.srv = httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad stub request", http.StatusBadRequest)
			return
		}
		name := ""
		if len(req.Args) > 0 {
			name, _ = req.Args[0].(string)
		}
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			_, _ = fmt.Fprintf(w, `[{"name":%q,"uuid":"d"},{"name":"Time Maps","uuid":"tm"}]`, e2eDay(0))
		case "logseq.Editor.get_page_blocks_tree":
			if name == "Time Maps" {
				_, _ = w.Write(mapRaw)
				return
			}
			_, _ = w.Write(taskRaw)
		case "logseq.Editor.get_page":
			_, _ = fmt.Fprintf(w, `{"name":%q,"uuid":"x","properties":null}`, name)
		case "logseq.Editor.update_block":
			f.record("update_block " + name)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected method", http.StatusNotFound)
		}
	}))
	f.srv.Start()
	return f
}

func (f *logseqFake) record(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, entry)
}

func (f *logseqFake) APIURL() string { return f.srv.URL + "/api" }

func (f *logseqFake) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func (f *logseqFake) Close() {
	if f.srv != nil {
		f.srv.Close()
	}
}

// fakeCalendar is the Google OAuth token endpoint plus Calendar v3 events:
// reads, per-calendar overrides and failures, and full CRUD with writes
// recorded for the no-op assertions.
type fakeCalendar struct {
	mu        sync.Mutex
	events    map[string]map[string]any
	order     []string
	reads     map[string]string
	status    map[string]int
	refreshes int
	lists     int
	readIDs   []string
	tokenSeen []string
	writes    []string
	srv       *httptest.Server
}

func newFakeCalendar() *fakeCalendar {
	return &fakeCalendar{
		events: map[string]map[string]any{},
		reads:  map[string]string{},
		status: map[string]int{},
	}
}

func (f *fakeCalendar) AddEvent(e map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, _ := e["id"].(string)
	if id == "" {
		id = fmt.Sprintf("evt-%d", len(f.events)+1)
		e["id"] = id
	}
	if _, seen := f.events[id]; !seen {
		f.order = append(f.order, id)
	}
	f.events[id] = e
}

func (f *fakeCalendar) SetRead(id, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads[id] = body
}

func (f *fakeCalendar) SetStatus(id string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = status
}

// Mount starts the fake and points the CLI's Google endpoints at it.
func (f *fakeCalendar) Mount(t testing.TB) {
	t.Helper()
	f.srv = httptest.NewTestServer(t, http.HandlerFunc(f.handle))
	f.srv.Start()
	pointGoogle(t, oauth2Endpoint(f.srv.URL), f.srv.URL)
}

func oauth2Endpoint(base string) oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}
}

func (f *fakeCalendar) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.URL.Path == "/token":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "grant", http.StatusBadRequest)
			return
		}
		f.refreshes++
		f.tokenSeen = append(f.tokenSeen, r.Form.Get("refresh_token"))
		writeJSON(w, map[string]any{"access_token": "1//fake-access", "token_type": "Bearer", "expires_in": 3600})

	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/events/"):
		_, id, _ := strings.CutLast(r.URL.Path, "/")
		e, ok := f.events[id]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no such event"}}`))
			return
		}
		writeJSON(w, e)

	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/events"):
		id := calendarID(r.URL.Path)
		f.lists++
		f.readIDs = append(f.readIDs, id)
		if status := f.status[id]; status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"fake calendar failure"}}`))
			return
		}
		if body, ok := f.reads[id]; ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		items := make([]map[string]any, 0, len(f.order))
		for _, eventID := range f.order {
			if e, ok := f.events[eventID]; ok {
				items = append(items, e)
			}
		}
		writeJSON(w, map[string]any{"items": items})

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
		e := readJSON(w, r)
		if e == nil {
			return
		}
		id, _ := e["id"].(string)
		if id == "" {
			id = fmt.Sprintf("evt-%d", len(f.events)+1)
			e["id"] = id
		}
		if _, seen := f.events[id]; !seen {
			f.order = append(f.order, id)
		}
		f.events[id] = e
		f.writes = append(f.writes, "POST "+id)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(e)

	case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/events/"):
		_, id, _ := strings.CutLast(r.URL.Path, "/")
		patch := readJSON(w, r)
		if patch == nil {
			return
		}
		cur := f.events[id]
		if cur == nil {
			cur = map[string]any{"id": id}
			f.order = append(f.order, id)
		}
		for k, v := range patch {
			cur[k] = v
		}
		f.events[id] = cur
		f.writes = append(f.writes, "PATCH "+id)
		writeJSON(w, cur)

	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/events/"):
		_, id, _ := strings.CutLast(r.URL.Path, "/")
		delete(f.events, id)
		f.order = slices.DeleteFunc(f.order, func(eventID string) bool { return eventID == id })
		f.writes = append(f.writes, "DELETE "+id)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func readJSON(w http.ResponseWriter, r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return nil
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeCalendar) WriteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func (f *fakeCalendar) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func (f *fakeCalendar) ResetWrites() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = nil
}

func (f *fakeCalendar) Lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

func (f *fakeCalendar) Refreshes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes
}

func (f *fakeCalendar) TokenSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokenSeen...)
}

func (f *fakeCalendar) ReadIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.readIDs...)
}

func (f *fakeCalendar) ResetReads() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readIDs = nil
	f.lists = 0
}

func (f *fakeCalendar) Close() {
	if f.srv != nil {
		f.srv.Close()
	}
}

// calendarID extracts the calendar from /calendars/{id}/events.
func calendarID(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if part == "calendars" && i+1 < len(parts) {
			id, _ := url.PathUnescape(parts[i+1])
			return id
		}
	}
	return ""
}
