package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"golang.org/x/oauth2"
)

// fakeGoogle is one httptest server for both endpoints the CLI needs: the
// OAuth token (which records the refresh it saw) and the Calendar v3
// events. Production gets Google's own; the white-box vars are what make
// this possible with no user-visible surface.
type fakeGoogle struct {
	refreshes int      // refresh grant calls
	lists     int      // events.list calls
	tokenSeen []string // the refresh tokens the client sent
}

func (f *fakeGoogle) mount(t *testing.T, eventsJSON string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			switch r.Form.Get("grant_type") {
			case "refresh_token":
				f.tokenSeen = append(f.tokenSeen, r.Form.Get("refresh_token"))
				f.refreshes++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "1//fake-access", "token_type": "Bearer", "expires_in": 3600,
				})
			default:
				t.Errorf("unexpected grant %q", r.Form.Get("grant_type"))
				http.Error(w, "grant", http.StatusBadRequest)
			}
		case strings.Contains(r.URL.Path, "/events"):
			f.lists++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(eventsJSON))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// pointBoth overlays the CLI's Google endpoints onto the fake and restores
// them when the test leaves.
func pointBoth(t *testing.T, endpoint oauth2.Endpoint, base string) {
	t.Helper()
	oldEndpoint, oldBase := googleEndpoint, googleCalendarBase
	googleEndpoint = endpoint
	googleCalendarBase = base
	t.Cleanup(func() { googleEndpoint, googleCalendarBase = oldEndpoint, oldBase })
}

// fixtureBusyDay names the weekday morning, two or so days out from now,
// whose 09:00-12:00 London stretch the busy event spans. Anchored to the
// wall clock the CLI itself injects, so the test never drifts out of the
// window and never races a midnight.
func fixtureBusyDay(t *testing.T) (start, end time.Time) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	day := time.Now().In(loc).AddDate(0, 0, 2)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, 9, 0, 0, 0, loc),
		time.Date(y, m, d, 12, 0, 0, 0, loc)
}

func TestForecastEndToEndWithFakeGoogle(t *testing.T) {
	busyStart, busyEnd := fixtureBusyDay(t)

	// One external event spanning the busy stretch, and one Managed event
	// Lodo itself owns (its span deliberate: inside the same window).
	events := fmt.Sprintf(`{"items": [
		{"id":"ext-meeting","status":"confirmed","summary":"External meeting","start":{"dateTime":%q},"end":{"dateTime":%q}},
		{"id":"lodo-is-mine","status":"confirmed","summary":"A Block","start":{"dateTime":%q},"end":{"dateTime":%q},"extendedProperties":{"private":{"lodo":"true"}}}
	]}`,
		busyStart.Format(time.RFC3339), busyEnd.Format(time.RFC3339),
		busyEnd.Format(time.RFC3339), busyEnd.Add(2*time.Hour).Format(time.RFC3339))

	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, events)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &bytes.Buffer{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}

	var view struct {
		Blocks []struct {
			TaskID string    `json:"task_id"`
			Start  time.Time `json:"start"`
			End    time.Time `json:"end"`
		} `json:"blocks"`
		Managed []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"managed_left_out"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, stdout.String())
	}

	// The client refreshed exactly once, with the stored refresh token.
	if len(fg.tokenSeen) != 1 || fg.tokenSeen[0] != "1//stored-refresh-token" {
		t.Fatalf("refresh grants = %v", fg.tokenSeen)
	}
	if fg.lists != 1 {
		t.Fatalf("events.list calls = %d, want 1 (an unfiltered window fits one page)", fg.lists)
	}

	// Busy shaped the plan (a block exists), the Managed event travelled
	// back as the note it keeps until schedule folds it in.
	if len(view.Blocks) == 0 {
		t.Fatal("no blocks: nothing should prevent the 30m Task's placement")
	}
	if len(view.Managed) != 1 || view.Managed[0].ID != "lodo-is-mine" || view.Managed[0].Summary != "A Block" {
		t.Fatalf("managed_left_out = %+v", view.Managed)
	}
	loc := busyStart.Location()
	for _, b := range view.Blocks {
		start, end := b.Start.In(loc), b.End.In(loc)
		if start.Before(busyEnd) && end.After(busyStart) {
			t.Fatalf("block %s..%s overlaps busy %s..%s", start, end, busyStart, busyEnd)
		}
	}

	// Same inputs, one run apart: byte-identical output.
	first := stdout.Bytes()
	next := &bytes.Buffer{}
	code = Run([]string{"--config", conf, "--json", "forecast"}, next, stderr)
	if code != 0 {
		t.Fatalf("second run: exit = %d, stderr %q", code, stderr.String())
	}
	if !bytes.Equal(first, next.Bytes()) {
		t.Fatalf("a second forecast moved something:\n%s\n---\n%s", first, next.String())
	}
}

// writeTestSetup writes the config and credentials files under the XDG
// roots the env points at, with a google section already authorized (its
// refresh token is what the fake token endpoint hands back an access for).
func writeTestSetup(t *testing.T, apiURL string) (conf, cred string) {
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
`, apiURL)
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

// miniApp is the smallest Logseq HTTP API: a Time Maps page with the
// default Mon-Fri 09:00-12:00 map, and one 30m TODO.
func miniApp(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			t.Fatalf("bad stub request: %v", err)
		}
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			_, _ = w.Write([]byte(`[{"name":"2026-01-01","uuid":"d"},{"name":"Time Maps","uuid":"tm"}]`))
		case "logseq.Editor.get_page_blocks_tree":
			if req.Args[0].(string) == "Time Maps" {
				_, _ = w.Write([]byte(`[{"uuid":"map","content":"Deep Work","properties":{"default":true},"children":[{"uuid":"w","content":"Mon-Fri 09:00-12:00"}]}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"uuid":"t-task-uuid","content":"TODO Write the milestone note","marker":"TODO","properties":{"duration":"30m"}}]`))
		case "logseq.Editor.get_page":
			_, _ = w.Write([]byte(`{"name":"` + req.Args[0].(string) + `","uuid":"x","properties":null}`))
		default:
			http.Error(w, "unexpected method", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The text rendering notes the Managed hole M4 leaves, in the same voice
// the JSON does.
func TestForecastTextNotesTheManagedHole(t *testing.T) {
	busyStart, busyEnd := fixtureBusyDay(t)
	events := fmt.Sprintf(`{"items": [{"id":"lodo-is-mine","status":"confirmed","summary":"A Block","start":{"dateTime":%q},"end":{"dateTime":%q},"extendedProperties":{"private":{"lodo":"true"}}}]}`,
		busyStart.Format(time.RFC3339), busyEnd.Format(time.RFC3339))

	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, events)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	for _, want := range []string{
		`Schedule that would be written to calendar "primary"`,
		"not counted as busy yet",
		"lodo-is-mine — A Block",
		"Write the milestone note",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("text output missing %q:\n%s", want, stdout.String())
		}
	}
	// Explanations only with --verbose; a shortfall would assert against it:
	// the 30m Task fits, so none exists.
	if strings.Contains(stdout.String(), "Shortfall:") || strings.Contains(stdout.String(), "Explanations:") {
		t.Fatalf("plain forecast must stay plain:\n%s", stdout.String())
	}

	stdout = &strings.Builder{}
	code = Run([]string{"--config", conf, "--verbose", "forecast"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Explanations:") {
		t.Fatalf("--verbose must add explanations:\n%s", stdout.String())
	}
}

func TestForecastCalendarFailureIsOperational(t *testing.T) {
	// An endpoint whose server has gone away: the refresh cannot fetch a
	// token, so the forecast refuses rather than plan against a silently
	// free calendar.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gone := srv.URL
	srv.Close()

	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	pointBoth(t, oauth2.Endpoint{AuthURL: gone + "/authorize", TokenURL: gone + "/token"}, gone)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "forecast"}, stdout, stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stdout %q)", code, stdout.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("a failed calendar prints no schedule: %q", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) == "" {
		t.Fatal("stderr must say why")
	}
}
