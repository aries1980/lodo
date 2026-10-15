package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// scheduleFake is a Calendar API fake with writes: token refresh, list,
// insert, patch, delete. Mutating calls are counted for the no-op assert.
type scheduleFake struct {
	mu     sync.Mutex
	events map[string]map[string]any
	writes []string
	lists  int
}

func (f *scheduleFake) mount(t *testing.T, initial []map[string]any) string {
	t.Helper()
	f.events = map[string]map[string]any{}
	for _, e := range initial {
		id, _ := e["id"].(string)
		f.events[id] = e
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "fake", "token_type": "Bearer", "expires_in": 3600,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/events"):
			f.lists++
			items := make([]map[string]any, 0, len(f.events))
			for _, e := range f.events {
				items = append(items, e)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			body, _ := io.ReadAll(r.Body)
			var e map[string]any
			_ = json.Unmarshal(body, &e)
			id, _ := e["id"].(string)
			f.writes = append(f.writes, "POST "+id)
			f.events[id] = e
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(e)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/events/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			body, _ := io.ReadAll(r.Body)
			var p map[string]any
			_ = json.Unmarshal(body, &p)
			cur := f.events[id]
			if cur == nil {
				cur = map[string]any{"id": id}
			}
			for k, v := range p {
				cur[k] = v
			}
			f.events[id] = cur
			f.writes = append(f.writes, "PATCH "+id)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cur)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/events/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			f.writes = append(f.writes, "DELETE "+id)
			delete(f.events, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *scheduleFake) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func TestScheduleWritesThenNoOp(t *testing.T) {
	f := &scheduleFake{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := f.mount(t, nil)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--json", "schedule"}, stdout, stderr); code != 0 {
		t.Fatalf("first schedule exit = %d, stderr %q", code, stderr.String())
	}
	var first struct {
		Blocks []struct {
			TaskID string `json:"task_id"`
		} `json:"blocks"`
		Writes []struct {
			Op string `json:"op"`
		} `json:"writes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil {
		t.Fatalf("first schedule JSON: %v\n%s", err, stdout.String())
	}
	if len(first.Blocks) == 0 || len(first.Writes) == 0 {
		t.Fatalf("first schedule must place and write: blocks=%d writes=%d", len(first.Blocks), len(first.Writes))
	}
	if got := f.writeCount(); got == 0 {
		t.Fatalf("fake saw no mutating calls")
	}

	// Second run: no-op, zero writes.
	f.mu.Lock()
	f.writes = nil
	f.mu.Unlock()
	next, stderr2 := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--json", "schedule"}, next, stderr2); code != 0 {
		t.Fatalf("second schedule exit = %d, stderr %q", code, stderr2.String())
	}
	var second struct {
		Writes []any `json:"writes"`
	}
	if err := json.Unmarshal(next.Bytes(), &second); err != nil {
		t.Fatalf("second schedule JSON: %v", err)
	}
	if len(second.Writes) != 0 {
		t.Fatalf("second schedule writes = %d, want 0 (no-op)", len(second.Writes))
	}
	if got := f.writeCount(); got != 0 {
		t.Fatalf("second run issued %d calendar writes, want 0", got)
	}

	// Third no-op run is byte-identical to the second: --json determinism.
	third := &bytes.Buffer{}
	if code := Run([]string{"--config", conf, "--json", "schedule"}, third, &strings.Builder{}); code != 0 {
		t.Fatalf("third schedule exit")
	}
	if !bytes.Equal(next.Bytes(), third.Bytes()) {
		t.Fatalf("no-op schedules differ:\n%s\n---\n%s", next.String(), third.String())
	}
}

func TestScheduleDryRunEqualsForecast(t *testing.T) {
	f := &scheduleFake{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := f.mount(t, nil)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	forecast := &bytes.Buffer{}
	if code := Run([]string{"--config", conf, "--json", "forecast"}, forecast, &strings.Builder{}); code != 0 {
		t.Fatalf("forecast exit")
	}
	dry := &bytes.Buffer{}
	if code := Run([]string{"--config", conf, "--json", "schedule", "--dry-run"}, dry, &strings.Builder{}); code != 0 {
		t.Fatalf("dry-run exit")
	}
	if !bytes.Equal(forecast.Bytes(), dry.Bytes()) {
		t.Fatalf("--dry-run must equal forecast:\n%s\n---\n%s", forecast.String(), dry.String())
	}
	if got := f.writeCount(); got != 0 {
		t.Fatalf("dry-run issued %d writes, want 0", got)
	}
}

func TestRefreshNeverFetchesTheGraph(t *testing.T) {
	f := &scheduleFake{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := f.mount(t, nil)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	// Break the Logseq endpoint after setup: refresh must still succeed
	// because it never resolves the graph.
	_ = api
	// Rewrite the config to point Logseq at a gone server.
	confBody := `
logseq:
  api_url: http://127.0.0.1:1/api
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 7
  timezone: Europe/London
calendar:
  provider: google
  id: primary
`
	writeFile(t, conf, confBody)

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "--json", "refresh"}, stdout, stderr); code != 0 {
		t.Fatalf("refresh with dead graph exit = %d, stderr %q (must not fetch the graph)", code, stderr.String())
	}
	var view struct {
		Dispositions []any `json:"dispositions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("refresh JSON: %v", err)
	}
	// Determinism: a second refresh is byte-identical.
	again := &bytes.Buffer{}
	if code := Run([]string{"--config", conf, "--json", "refresh"}, again, &strings.Builder{}); code != 0 {
		t.Fatalf("second refresh exit")
	}
	if !bytes.Equal(stdout.Bytes(), again.Bytes()) {
		t.Fatalf("refresh JSON not deterministic:\n%s\n---\n%s", stdout.String(), again.String())
	}
}

func TestExitCodeMatrix(t *testing.T) {
	// Schedule without google creds: exit 1 (operational).
	f := &scheduleFake{}
	_ = f
	api := miniApp(t)
	// Minimal setup without the google section: reuse the external-test
	// helper shape by writing a credentials file with only logseq.
	conf, cred := writeTestSetup(t, api)
	stripGoogle(t, cred)
	// Point endpoints at a gone server so any calendar attempt fails fast;
	// the missing google section must fail before any network.
	pointBoth(t, oauth2.Endpoint{AuthURL: "http://127.0.0.1:1/authorize", TokenURL: "http://127.0.0.1:1/token"}, "http://127.0.0.1:1")

	if code := Run([]string{"--config", conf, "schedule"}, &strings.Builder{}, &strings.Builder{}); code != 1 {
		t.Fatalf("schedule without google exit = %d, want 1", code)
	}
	if code := Run([]string{"--config", conf, "refresh"}, &strings.Builder{}, &strings.Builder{}); code != 1 {
		t.Fatalf("refresh without google exit = %d, want 1", code)
	}

	// Orphaned event → schedule --strict exits 2, plain exits 0.
	f2 := &scheduleFake{}
	base := f2.mount(t, []map[string]any{{
		"id": "lodo-orphan", "summary": "ghost",
		"start":              map[string]any{"dateTime": time.Now().Add(48 * time.Hour).Format(time.RFC3339)},
		"end":                map[string]any{"dateTime": time.Now().Add(48*time.Hour + 30*time.Minute).Format(time.RFC3339)},
		"extendedProperties": map[string]any{"private": map[string]any{"lodo": "true"}},
	}})
	conf2, _ := writeTestSetup(t, api)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)
	if code := Run([]string{"--config", conf2, "schedule", "--strict"}, &strings.Builder{}, &strings.Builder{}); code != 2 {
		t.Fatalf("schedule --strict with orphan exit = %d, want 2", code)
	}
	if code := Run([]string{"--config", conf2, "schedule"}, &strings.Builder{}, &strings.Builder{}); code != 0 {
		t.Fatalf("schedule with orphan exit = %d, want 0 (reported, not failed)", code)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stripGoogle(t *testing.T, credPath string) {
	t.Helper()
	raw, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	delete(v, "google")
	out, _ := json.Marshal(v)
	if err := os.WriteFile(credPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
