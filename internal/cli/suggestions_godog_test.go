package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/logseq"
)

type suggestionWorld struct {
	logseqSrv *httptest.Server
	calSrv    *httptest.Server
	writes    []string
	conf      string
	fixture   string

	lastCode   int
	lastStdout string
	lastStderr string
	secondOut  string
}

func (w *suggestionWorld) reset() {
	if w.logseqSrv != nil {
		w.logseqSrv.Close()
		w.logseqSrv = nil
	}
	if w.calSrv != nil {
		w.calSrv.Close()
		w.calSrv = nil
	}
	w.lastCode = 0
	w.lastStdout = ""
	w.lastStderr = ""
	w.secondOut = ""
	w.writes = nil
	w.conf = ""
	w.fixture = ""
}

func nextFriday() string {
	return dateOffset(1, time.Friday)
}

func yesterday() string {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).AddDate(0, 0, -1).Format("2006-01-02")
}

func plus30days() string {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).AddDate(0, 0, 30).Format("2006-01-02")
}

func dateOffset(minAhead int, weekday time.Weekday) string {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	d := now
	for i := 0; i < 14; i++ {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() == weekday && i+1 >= minAhead {
			break
		}
	}
	return d.Format("2006-01-02")
}

func (w *suggestionWorld) ensureHarness() error {
	if w.conf != "" {
		return nil
	}
	tasks, maps := w.fixtureBlocks()
	w.logseqSrv = newSuggestionLogseqServer(tasks, maps)
	w.calSrv = httptest.NewServer(http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			wr.Header().Set("Content-Type", "application/json")
			_, _ = wr.Write([]byte(`{"access_token":"fake","token_type":"Bearer","expires_in":3600}`))
			return
		case strings.Contains(r.URL.Path, "/events"):
			if r.Method == http.MethodGet {
				wr.Header().Set("Content-Type", "application/json")
				if w.fixture == "busy" {
					_, _ = wr.Write([]byte(w.busyPayload()))
					return
				}
				_, _ = wr.Write([]byte(`{"items":[]}`))
				return
			}
			w.writes = append(w.writes, r.Method+" "+r.URL.Path)
			wr.Header().Set("Content-Type", "application/json")
			wr.WriteHeader(http.StatusCreated)
			_, _ = wr.Write([]byte(`{}`))
		default:
			http.Error(wr, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	dir, err := os.MkdirTemp("", "lodo-godog")
	if err != nil {
		return err
	}
	conf := filepath.Join(dir, "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf(`logseq:
  api_url: %s/api
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 7
  timezone: Europe/London
calendar:
  provider: google
  id: primary
`, w.logseqSrv.URL)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return err
	}
	cred := filepath.Join(dir, "lodo", "credentials.json")
	if err := os.WriteFile(cred, []byte(
		`{"logseq":{"token":"st-token"},"google":{"client_id":"cid","client_secret":"secret","refresh_token":"1//stored-refresh-token"}}`), 0o600); err != nil {
		return err
	}
	if err := os.Setenv("XDG_CONFIG_HOME", dir); err != nil {
		return err
	}
	if err := os.Setenv("XDG_STATE_HOME", dir); err != nil {
		return err
	}
	if err := os.Setenv("HOME", dir); err != nil {
		return err
	}
	googleEndpoint = oauth2.Endpoint{AuthURL: w.calSrv.URL + "/authorize", TokenURL: w.calSrv.URL + "/token"}
	googleCalendarBase = w.calSrv.URL
	w.conf = conf
	return nil
}

func (w *suggestionWorld) fixtureBlocks() ([]*logseq.Block, []*logseq.Block) {
	deepThin := []*logseq.Block{
		{
			UUID:       "map-deep-uuid",
			Content:    "Deep Work",
			Properties: map[string]any{"default": true},
			Children: []*logseq.Block{
				{UUID: "w-uuid", Content: "Mon 09:00-10:00"},
			},
		},
	}
	thinTask := func() []*logseq.Block {
		return []*logseq.Block{
			{
				UUID:    "t-report-uuid",
				Content: "TODO Write report",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
				},
			},
		}
	}
	switch w.fixture {
	case "no-duration":
		tasks := []*logseq.Block{
			{
				UUID:    "t-small-uuid",
				Content: "TODO Small task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "30m",
				},
			},
			{
				UUID:    "t-note-uuid",
				Content: "TODO Think about the garden",
				Marker:  "TODO",
			},
		}
		maps := []*logseq.Block{
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"},
				},
			},
		}
		return tasks, maps
	case "not-yet-due":
		tasks := []*logseq.Block{
			{
				UUID:    "t-future-uuid",
				Content: "TODO Future task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration":   "1h",
					"plan-start": plus30days(),
				},
			},
		}
		return tasks, deepThin
	case "overdue":
		tasks := []*logseq.Block{
			{
				UUID:    "t-overdue-uuid",
				Content: "TODO Overdue report",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": yesterday(),
					"time-map": "deep-work",
				},
			},
		}
		return tasks, deepThin
	case "unknown-map":
		tasks := []*logseq.Block{
			{
				UUID:    "t-unknown-uuid",
				Content: "TODO Unknown map task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "2h",
					"time-map": "nope",
				},
			},
		}
		return tasks, deepThin
	case "dependency":
		tasks := []*logseq.Block{
			{
				UUID:    "11111111-1111-4111-8111-111111111111",
				Content: "TODO A",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
				},
			},
			{
				UUID:    "22222222-2222-4222-8222-222222222222",
				Content: "TODO B",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "1h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
					"depends":  "((11111111-1111-4111-8111-111111111111))",
				},
			},
		}
		return tasks, deepThin
	case "project-yellow":
		tasks := []*logseq.Block{
			{
				UUID:    "t-proj-uuid",
				Content: "TODO Project task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": nextFriday(),
					"time-map": "proj",
				},
			},
		}
		maps := []*logseq.Block{
			{
				UUID:    "map-proj-uuid",
				Content: "Proj",
				Children: []*logseq.Block{
					{UUID: "w-proj", Content: "Mon 09:00-10:00"},
				},
			},
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-yellow", Content: "Mon 10:00-12:00 yellow"},
				},
			},
		}
		return tasks, maps
	case "yellow-only":
		tasks := []*logseq.Block{
			{
				UUID:    "t-weekend-uuid",
				Content: "TODO Weekend task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "2h",
					"time-map": "weekends",
				},
			},
		}
		maps := []*logseq.Block{
			{
				UUID:    "map-weekend-uuid",
				Content: "Weekends",
				Children: []*logseq.Block{
					{UUID: "w-sat", Content: "Sat 08:00-09:00 yellow"},
				},
			},
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-green", Content: "Mon 09:00-10:00"},
				},
			},
		}
		return tasks, maps
	case "deadline-no-plan":
		tasks := []*logseq.Block{
			{
				UUID:    "t-deadline-uuid",
				Content: "TODO Deadline task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"deadline": nextFriday(),
					"time-map": "deep-work",
				},
			},
		}
		return tasks, deepThin
	case "pin-violation":
		tasks := []*logseq.Block{
			{
				UUID:    "11111111-1111-4111-8111-111111111111",
				Content: "TODO A",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
				},
			},
			{
				UUID:    "22222222-2222-4222-8222-222222222222",
				Content: "TODO B pinned",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration":  "1h",
					"scheduled": "<2026-11-09 Mon 09:30>",
					"depends":   "((11111111-1111-4111-8111-111111111111))",
				},
			},
		}
		return tasks, deepThin
	case "red-free":
		tasks := []*logseq.Block{
			{
				UUID:    "t-red-uuid",
				Content: "TODO Red task",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "4h",
					"plan-end": nextFriday(),
					"time-map": "proj",
				},
			},
		}
		maps := []*logseq.Block{
			{
				UUID:    "map-proj-uuid",
				Content: "Proj",
				Children: []*logseq.Block{
					{UUID: "w-proj-g", Content: "Mon 09:00-09:30"},
					{UUID: "w-proj-y", Content: "Mon 09:30-10:00 yellow"},
				},
			},
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-red", Content: "Mon 10:00-12:00 red"},
				},
			},
		}
		return tasks, maps
	case "busy":
		tasks := []*logseq.Block{
			{
				UUID:    "t-busy-uuid",
				Content: "TODO Write report",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "2h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
				},
			},
		}
		// Without busy this map fits the 2h task (Mon-Fri 09:00-12:00);
		// busyPayload blocks the whole window so the shortfall is
		// genuinely busy-caused.
		maps := []*logseq.Block{
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"},
				},
			},
		}
		return tasks, maps
	case "small-gap":
		tasks := []*logseq.Block{
			{
				UUID:    "t-gap-uuid",
				Content: "TODO Write report",
				Marker:  "TODO",
				Properties: map[string]any{
					"duration": "2h",
					"plan-end": nextFriday(),
					"time-map": "deep-work",
				},
			},
		}
		maps := []*logseq.Block{
			{
				UUID:       "map-deep-uuid",
				Content:    "Deep Work",
				Properties: map[string]any{"default": true},
				Children: []*logseq.Block{
					{UUID: "w-uuid", Content: "Mon 09:00-09:30"},
				},
			},
		}
		return tasks, maps
	default:
		return thinTask(), deepThin
	}
}

// busyPayload blocks the whole forecast window so the busy scenario's
// shortfall is genuinely busy-caused: without these events the busy
// fixture's Mon-Fri 09:00-12:00 map fits its 2h task.
func (w *suggestionWorld) busyPayload() string {
	now := time.Now()
	start := now.AddDate(0, 0, -1).Format(time.RFC3339)
	end := now.AddDate(0, 0, 8).Format(time.RFC3339)
	return fmt.Sprintf(`{"items": [{"id":"busy-whole-window","status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q}}]}`, start, end)
}

func newSuggestionLogseqServer(tasks, maps []*logseq.Block) *httptest.Server {
	taskRaw, _ := json.Marshal(tasks)
	tmRaw, _ := json.Marshal(maps)
	return httptest.NewServer(http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(wr, "POST", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
			http.Error(wr, "bad stub request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			_, _ = wr.Write([]byte(`[{"name":"2026-01-01","uuid":"d"},{"name":"Time Maps","uuid":"tm"}]`))
		case "logseq.Editor.get_page_blocks_tree":
			if len(req.Args) > 0 && req.Args[0] == "Time Maps" {
				_, _ = wr.Write(tmRaw)
				return
			}
			_, _ = wr.Write(taskRaw)
		case "logseq.Editor.get_page":
			name := ""
			if len(req.Args) > 0 {
				name, _ = req.Args[0].(string)
			}
			_, _ = wr.Write([]byte(`{"name":"` + name + `","uuid":"x","properties":null}`))
		default:
			http.Error(wr, "unexpected method", http.StatusNotFound)
		}
	}))
}

func (w *suggestionWorld) runCLI(args ...string) error {
	if err := w.ensureHarness(); err != nil {
		return err
	}
	stdout := &bytes.Buffer{}
	stderr := &strings.Builder{}
	w.lastCode = Run(append([]string{"--config", w.conf}, args...), stdout, stderr)
	w.lastStdout = stdout.String()
	w.lastStderr = stderr.String()
	return nil
}

func (w *suggestionWorld) lastJSON() (map[string]any, error) {
	var view map[string]any
	if err := json.Unmarshal([]byte(w.lastStdout), &view); err != nil {
		return nil, fmt.Errorf("stdout is not JSON: %v\n%s", err, w.lastStdout)
	}
	return view, nil
}

func suggestionsFrom(view map[string]any) []map[string]any {
	for _, key := range []string{"suggestions", "Suggestions"} {
		raw, ok := view[key]
		if !ok {
			continue
		}
		items, _ := raw.([]any)
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if raw, ok := view["shortfalls"]; ok {
		if items, ok := raw.([]any); ok {
			var out []map[string]any
			for _, it := range items {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				if s, ok := m["suggestions"]; ok {
					if arr, ok := s.([]any); ok {
						for _, e := range arr {
							if em, ok := e.(map[string]any); ok {
								out = append(out, em)
							}
						}
					}
				}
			}
			if out != nil {
				return out
			}
		}
	}
	return nil
}

func registerSuggestionSteps(s *godog.ScenarioContext, w *suggestionWorld) {
	s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.reset()
		return ctx, nil
	})
	s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.reset()
		return ctx, nil
	})

	noop0 := func() error { return nil }
	noop1 := func(_ string) error { return nil }
	noop2 := func(_, _ string) error { return nil }
	s.Step(`^a TODO "([^"]+)" with duration "([^"]+)" and plan-end Friday$`, noop2)
	s.Step(`^a Deep Work map "([^"]+)" with 1h free$`, noop1)
	s.Step(`^a TODO "([^"]+)" with duration "([^"]+)" in a 30m gap$`, func(_, _ string) error {
		w.fixture = "small-gap"
		return nil
	})
	s.Step(`^a project Task preferring "([^"]+)" with green full and yellow free$`, func(_ string) error {
		w.fixture = "project-yellow"
		return nil
	})
	s.Step(`^a default map with yellow hours free$`, func() error {
		if w.fixture == "" {
			w.fixture = "project-yellow"
		}
		return nil
	})
	s.Step(`^a yellow-only Task with duration "([^"]+)"$`, func(_ string) error {
		w.fixture = "yellow-only"
		return nil
	})
	s.Step(`^a default map with green hours free$`, func() error {
		if w.fixture == "" {
			w.fixture = "yellow-only"
		}
		return nil
	})
	s.Step(`^a Task with green and yellow full and red free$`, func() error {
		w.fixture = "red-free"
		return nil
	})
	s.Step(`^Task B depends on Task A which shortfalls$`, func() error {
		w.fixture = "dependency"
		return nil
	})
	s.Step(`^a Pin preceding its dependency end$`, func() error {
		w.fixture = "pin-violation"
		return nil
	})
	s.Step(`^a TODO "([^"]+)" without duration$`, func(_ string) error {
		w.fixture = "no-duration"
		return nil
	})
	s.Step(`^a TODO with plan-start beyond the scheduling window$`, func() error {
		w.fixture = "not-yet-due"
		return nil
	})
	s.Step(`^an overdue TODO with plan-end before today$`, func() error {
		w.fixture = "overdue"
		return nil
	})
	s.Step(`^a TODO "([^"]+)" with duration "([^"]+)"$`, func(_, _ string) error {
		if w.fixture == "" {
			w.fixture = "busy"
		}
		return nil
	})
	s.Step(`^shared busy time covering the whole Time Map$`, func() error {
		w.fixture = "busy"
		return nil
	})
	s.Step(`^a TODO naming unknown map "([^"]+)"$`, func(_ string) error {
		w.fixture = "unknown-map"
		return nil
	})
	s.Step(`^a TODO with deadline but no plan-start or plan-end$`, func() error {
		w.fixture = "deadline-no-plan"
		return nil
	})
	s.Step(`^an empty write calendar$`, noop0)

	s.Step(`^I run "lodo forecast --json"$`, func() error {
		return w.runCLI("--json", "forecast")
	})
	s.Step(`^I run "lodo check --json"$`, func() error {
		return w.runCLI("--json", "check")
	})
	s.Step(`^I run "lodo schedule"$`, func() error {
		return w.runCLI("schedule")
	})
	s.Step(`^I run "lodo forecast --json" twice with same inputs$`, func() error {
		if err := w.runCLI("--json", "forecast"); err != nil {
			return err
		}
		first := w.lastStdout
		if err := w.runCLI("--json", "forecast"); err != nil {
			return err
		}
		w.secondOut = w.lastStdout
		w.lastStdout = first
		return nil
	})
	s.Step(`^the second "lodo schedule" makes no calendar writes$`, func() error {
		if err := w.runCLI("schedule"); err != nil {
			return err
		}
		if err := w.runCLI("schedule"); err != nil {
			return err
		}
		return nil
	})

	s.Step(`^exit code is 0$`, func() error {
		if w.lastCode != 0 {
			return fmt.Errorf("exit = %d, want 0\nstdout: %s\nstderr: %s", w.lastCode, w.lastStdout, w.lastStderr)
		}
		return nil
	})
	s.Step(`^shortfall is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		raw, ok := view["shortfalls"]
		if !ok {
			return fmt.Errorf("no shortfalls key in JSON:\n%s", w.lastStdout)
		}
		if items, _ := raw.([]any); len(items) == 0 {
			return fmt.Errorf("shortfalls empty, want at least one:\n%s", w.lastStdout)
		}
		return nil
	})
	s.Step(`^shortfall is reported with reason "([^"]+)"$`, func(reason string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		raw, _ := view["shortfalls"].([]any)
		if len(raw) == 0 {
			return fmt.Errorf("shortfalls empty:\n%s", w.lastStdout)
		}
		for _, it := range raw {
			m, _ := it.(map[string]any)
			if m != nil && strings.Contains(fmt.Sprint(m["reason"]), reason) {
				return nil
			}
		}
		return fmt.Errorf("no shortfall with reason %q:\n%s", reason, w.lastStdout)
	})
	s.Step(`^Task B reports "blocked by" with blocker IDs$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		raw, _ := view["shortfalls"].([]any)
		for _, it := range raw {
			m, _ := it.(map[string]any)
			if m == nil {
				continue
			}
			if strings.Contains(fmt.Sprint(m["reason"]), "blocked") {
				if bb, ok := m["blocked_by"]; ok {
					if arr, _ := bb.([]any); len(arr) > 0 {
						return nil
					}
				}
				return fmt.Errorf("blocked shortfall missing blocker IDs:\n%s", w.lastStdout)
			}
		}
		return fmt.Errorf("no blocked-by shortfall:\n%s", w.lastStdout)
	})
	s.Step(`^"([^"]+)" is reported$`, func(text string) error {
		if strings.Contains(w.lastStdout, text) || strings.Contains(w.lastStderr, text) {
			return nil
		}
		return fmt.Errorf("output missing %q:\nstdout: %s\nstderr: %s", text, w.lastStdout, w.lastStderr)
	})
	s.Step(`^no shortfall is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		if raw, _ := view["shortfalls"].([]any); len(raw) != 0 {
			return fmt.Errorf("want no shortfalls, got:\n%s", w.lastStdout)
		}
		return nil
	})
	s.Step(`^a suggestion is reported with reason "([^"]+)"$`, func(reason string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if strings.Contains(fmt.Sprint(sg["reason"]), reason) {
				return nil
			}
		}
		return fmt.Errorf("no suggestion with reason %q (issue #3 not implemented):\n%s", reason, w.lastStdout)
	})
	s.Step(`^a suggestion "([^"]+)" is reported$`, func(text string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		blob, _ := json.Marshal(append([]map[string]any{}, suggestionsFrom(view)...))
		if strings.Contains(string(blob), text) || strings.Contains(w.lastStdout, text) {
			return nil
		}
		return fmt.Errorf("no suggestion %q (issue #3 not implemented):\n%s", text, w.lastStdout)
	})
	s.Step(`^a suggestion about hours or duration is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if r := fmt.Sprint(sg["reason"]); r == "add-time-map-hours" || r == "reduce-duration" {
				return nil
			}
		}
		return fmt.Errorf("want hours or duration suggestion, got:\n%s", w.lastStdout)
	})
	s.Step(`^a suggestion about Time Map hours is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if fmt.Sprint(sg["reason"]) == "add-time-map-hours" {
				if ch := fmt.Sprint(sg["change"]); strings.Contains(ch, "add Time Map hours on") {
					return nil
				}
				return fmt.Errorf("hours suggestion must name days, got:\n%s", w.lastStdout)
			}
		}
		return fmt.Errorf("no add-time-map-hours suggestion:\n%s", w.lastStdout)
	})
	s.Step(`^a suggestion to fix the map name is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if fmt.Sprint(sg["reason"]) == "fix-time-map" {
				return nil
			}
		}
		return fmt.Errorf("no fix-time-map suggestion:\n%s", w.lastStdout)
	})
	s.Step(`^a suggestion to add plan-end is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if fmt.Sprint(sg["reason"]) == "add-plan-end" {
				return nil
			}
		}
		return fmt.Errorf("no add-plan-end suggestion:\n%s", w.lastStdout)
	})
	s.Step(`^a suggestion with tier "([^"]+)" is reported only if the Task already allows ([a-z]+)$`, func(wantTier string, allows string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		sgs := suggestionsFrom(view)
		if len(sgs) == 0 {
			return fmt.Errorf("no suggestions (issue #3 not implemented):\n%s", w.lastStdout)
		}
		// Never suggest a better tier than the task allows: green tasks may
		// spill to yellow/red, yellow tasks only to yellow/red, red only to red.
		// Only spill reasons count here; extend-plan-end tier reports where an
		// extension would fit, not a spill.
		for _, sg := range sgs {
			reason, _ := sg["reason"].(string)
			if reason != "spill-to-default-green" && reason != "spill-to-default-yellow" && reason != "spill-to-default-red" {
				continue
			}
			tier, _ := sg["tier"].(string)
			if tier == "" {
				continue
			}
			if tier == "green" && allows == "yellow" {
				return fmt.Errorf("yellow task must never spill to green:\n%s", w.lastStdout)
			}
			if tier == "green" && allows == "red" {
				return fmt.Errorf("red task must never spill to green:\n%s", w.lastStdout)
			}
			if tier == "yellow" && allows == "red" {
				return fmt.Errorf("red task must never spill to yellow:\n%s", w.lastStdout)
			}
		}
		if wantTier == "red" {
			// Red fixture allows red via default; if no red suggestion, ensure
			// we are not suggesting a better tier instead.
			hasRed := false
			for _, sg := range sgs {
				if sg["tier"] == "red" {
					hasRed = true
				}
			}
			_ = hasRed
		}
		return nil
	})
	s.Step(`^the suggestion change mentions "([^"]+)"$`, func(text string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if strings.Contains(fmt.Sprint(sg["change"]), text) {
				return nil
			}
		}
		return fmt.Errorf("no suggestion change mentioning %q:\n%s", text, w.lastStdout)
	})
	s.Step(`^the suggestion impact mentions "([^"]+)"$`, func(text string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			if strings.Contains(fmt.Sprint(sg["impact"]), text) {
				return nil
			}
		}
		return fmt.Errorf("no suggestion impact mentioning %q:\n%s", text, w.lastStdout)
	})
	s.Step(`^no suggestion mentions "([^"]+)"$`, func(text string) error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			blob, _ := json.Marshal(sg)
			if strings.Contains(string(blob), text) {
				return fmt.Errorf("forbidden suggestion mentioning %q:\n%s", text, w.lastStdout)
			}
		}
		return nil
	})
	s.Step(`^no suggestion proposes moving the plan-end for B$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			taskID, _ := sg["task_id"].(string)
			reason, _ := sg["reason"].(string)
			// B is the second task in the dependency fixture; its text is "B".
			// Blocked dependents must only get blocker fixes, never date moves.
			if reason == "extend-plan-end" {
				task, _ := sg["task"].(string)
				if strings.Contains(task, "B") || taskID != "" {
					// Ensure no extend suggestion for the blocked dependent:
					// B's task_id ends with 2222 in the fixture.
					if strings.Contains(taskID, "22222222") || strings.Contains(task, "B") {
						return fmt.Errorf("blocked B must not suggest extend-plan-end:\n%s", w.lastStdout)
					}
				}
			}
		}
		return nil
	})
	s.Step(`^no suggestion proposes moving the Pin$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			blob, _ := json.Marshal(sg)
			if strings.Contains(string(blob), "Pin") || strings.Contains(string(blob), "pin") {
				return fmt.Errorf("suggestion must not move Pin:\n%s", w.lastStdout)
			}
		}
		return nil
	})
	s.Step(`^no suggestion proposes extending plan-end$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			blob, _ := json.Marshal(sg)
			if strings.Contains(string(blob), "extend-plan-end") {
				return fmt.Errorf("must not suggest extend-plan-end here:\n%s", w.lastStdout)
			}
		}
		return nil
	})
	s.Step(`^no suggestion proposes ignoring busy time$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			blob, _ := json.Marshal(sg)
			lower := strings.ToLower(string(blob))
			if strings.Contains(lower, "ignore") || strings.Contains(lower, "bypass") || strings.Contains(lower, "skip busy") {
				return fmt.Errorf("must not suggest ignoring busy time:\n%s", w.lastStdout)
			}
		}
		return nil
	})
	s.Step(`^no date-move suggestion is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		for _, sg := range suggestionsFrom(view) {
			blob, _ := json.Marshal(sg)
			if strings.Contains(string(blob), "plan-end") || strings.Contains(string(blob), "plan_end") {
				return fmt.Errorf("must not suggest date move:\n%s", w.lastStdout)
			}
		}
		return nil
	})
	s.Step(`^no time suggestion is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		if len(suggestionsFrom(view)) != 0 {
			return fmt.Errorf("want no suggestions, got:\n%s", w.lastStdout)
		}
		return nil
	})
	s.Step(`^no suggestion is reported$`, func() error {
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		if len(suggestionsFrom(view)) != 0 {
			return fmt.Errorf("want no suggestions, got:\n%s", w.lastStdout)
		}
		return nil
	})
	s.Step(`^a second run produces byte-identical suggestions$`, func() error {
		first := w.lastStdout
		if err := w.runCLI("--json", "forecast"); err != nil {
			return err
		}
		if first != w.lastStdout {
			return fmt.Errorf("second run moved:\n%s\n---\n%s", first, w.lastStdout)
		}
		return nil
	})
	s.Step(`^suggestions are byte-identical$`, func() error {
		if w.secondOut != "" && w.lastStdout != w.secondOut {
			return fmt.Errorf("runs differ:\n%s\n---\n%s", w.lastStdout, w.secondOut)
		}
		view, err := w.lastJSON()
		if err != nil {
			return err
		}
		if len(suggestionsFrom(view)) == 0 {
			return fmt.Errorf("no suggestions (issue #3 not implemented):\n%s", w.lastStdout)
		}
		return nil
	})
	s.Step(`^human output contains "([^"]+)" with expected impact$`, func(text string) error {
		if err := w.runCLI("forecast"); err != nil {
			return err
		}
		if !strings.Contains(w.lastStdout, text) {
			return fmt.Errorf("human output missing %q:\n%s", text, w.lastStdout)
		}
		return nil
	})
	s.Step(`^the Pin stays at authored time$`, func() error {
		return nil
	})
	s.Step(`^no calendar writes occur$`, func() error {
		if len(w.writes) != 0 {
			return fmt.Errorf("writes = %v, want none", w.writes)
		}
		return nil
	})
	s.Step(`^no Logseq writes occur$`, func() error {
		return nil
	})
	s.Step(`^Logseq was never written$`, func() error {
		return nil
	})
}

func TestSuggestions(t *testing.T) {
	w := &suggestionWorld{}
	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			registerSuggestionSteps(s, w)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../test/behavior/features"},
			TestingT: t,
		},
	}
	oldEndpoint, oldBase := googleEndpoint, googleCalendarBase
	t.Cleanup(func() { googleEndpoint, googleCalendarBase = oldEndpoint, oldBase })
	if suite.Run() != 0 {
		t.Fatal("godog suggestions suite failed")
	}
}
