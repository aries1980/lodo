package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// calendarFake serves token refresh plus per-calendar events.list, recording
// which calendars were read in order.
type calendarFake struct {
	events map[string]string // calendar ID -> events.list body
	status map[string]int    // calendar ID -> failure status (403/404)
	ids    []string          // calendar IDs read, in order
	lists  int
}

func (f *calendarFake) mount(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "fake", "token_type": "Bearer", "expires_in": 3600,
			})
		case strings.Contains(r.URL.Path, "/events") && !strings.Contains(r.URL.Path, "/events/"):
			f.lists++
			id := calendarID(r.URL.Path)
			f.ids = append(f.ids, id)
			if code, ok := f.status[id]; ok && code != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":{"message":"denied"}}`))
				return
			}
			body := f.events[id]
			if body == "" {
				body = `{"items": []}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

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

// writeSetupWithReads writes config with the given read_calendars block.
func writeSetupWithReads(t *testing.T, apiURL, readsYAML string) (conf, cred string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)

	conf = filepath.Join(dir, "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o700); err != nil {
		t.Fatal(err)
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
`, apiURL, readsYAML)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cred = filepath.Join(dir, "lodo", "credentials.json")
	if err := os.WriteFile(cred, []byte(
		`{"logseq":{"token":"st-token"},"google":{"client_id":"cid","client_secret":"secret","refresh_token":"1//stored-refresh-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf, cred
}

func TestReadCalendarFlagReplacesConfig(t *testing.T) {
	api := miniApp(t)
	conf, _ := writeSetupWithReads(t, api, "  read_calendars:\n    - a@example.com\n    - b@example.com\n")
	f := &calendarFake{events: map[string]string{}}
	base := f.mount(t)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	// No flag: write plus both config reads, in order.
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr); code != 0 {
		t.Fatalf("forecast exit = %d, stderr %q", code, stderr.String())
	}
	if diff := len(f.ids); diff != 3 {
		t.Fatalf("reads = %q, want write plus two config reads", f.ids)
	}
	if f.ids[1] != "a@example.com" || f.ids[2] != "b@example.com" {
		t.Fatalf("reads = %q, want config order", f.ids)
	}
	var view struct {
		Busy struct {
			WriteCalendar string `json:"write_calendar"`
			Sources       []struct {
				CalendarID string `json:"calendar_id"`
				Status     string `json:"status"`
			} `json:"sources"`
		} `json:"busy_sources"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(view.Busy.Sources) != 2 {
		t.Fatalf("busy_sources = %+v", view.Busy)
	}

	// One flag replaces the whole list.
	f.ids = nil
	f.lists = 0
	stdout.Reset()
	if code := Run([]string{"--config", conf, "--json", "forecast", "--read-calendar", "c@example.com"}, stdout, &strings.Builder{}); code != 0 {
		t.Fatalf("forecast with flag exit")
	}
	if len(f.ids) != 2 || f.ids[1] != "c@example.com" {
		t.Fatalf("flag reads = %q, want write plus c only", f.ids)
	}

	// Repeatable: two flags, config order is flag order.
	f.ids = nil
	stdout.Reset()
	if code := Run([]string{"--config", conf, "--json", "forecast",
		"--read-calendar", "c@example.com", "--read-calendar", "d@example.com"}, stdout, &strings.Builder{}); code != 0 {
		t.Fatalf("forecast with two flags exit")
	}
	if len(f.ids) != 3 || f.ids[1] != "c@example.com" || f.ids[2] != "d@example.com" {
		t.Fatalf("two-flag reads = %q", f.ids)
	}

	// The write ID in the flag is silently dropped.
	f.ids = nil
	stdout.Reset()
	if code := Run([]string{"--config", conf, "--json", "forecast", "--read-calendar", "primary"}, stdout, &strings.Builder{}); code != 0 {
		t.Fatalf("forecast with write-ID flag exit")
	}
	if len(f.ids) != 1 {
		t.Fatalf("write-ID flag reads = %q, want write alone", f.ids)
	}
}

func TestForecastBusySourcesLineAndSkippedWarning(t *testing.T) {
	api := miniApp(t)
	conf, _ := writeSetupWithReads(t, api, "  read_calendars:\n    - good@example.com\n    - bad@example.com\n")
	busyStart, busyEnd := fixtureBusyDay(t)
	good := fmt.Sprintf(`{"items": [{"id":"g","status":"confirmed","start":{"dateTime":%q},"end":{"dateTime":%q}}]}`,
		busyStart.Format(time.RFC3339), busyEnd.Format(time.RFC3339))
	f := &calendarFake{
		events: map[string]string{
			"primary":          `{"items": []}`,
			"good@example.com": good,
		},
		status: map[string]int{"bad@example.com": http.StatusForbidden},
	}
	base := f.mount(t)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &strings.Builder{}
	if code := Run([]string{"--config", conf, "forecast"}, stdout, &strings.Builder{}); code != 0 {
		t.Fatalf("forecast exit")
	}
	out := stdout.String()
	if !strings.Contains(out, "busy sources:") || !strings.Contains(out, "primary") ||
		!strings.Contains(out, "good@example.com") || !strings.Contains(out, "bad@example.com skipped:403") {
		t.Fatalf("busy sources line missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "bad@example.com") ||
		!strings.Contains(out, "free/busy-only") {
		t.Fatalf("skipped warning missing:\n%s", out)
	}
}
