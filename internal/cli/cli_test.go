package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/wire"
)

// missingDurationGraph is check's fixture: one schedulable Task and one TODO
// without duration, so a plain check has something to report.
func missingDurationGraph() []*logseq.Block {
	return []*logseq.Block{
		{
			UUID:       "t-task-uuid",
			Content:    "TODO Write the milestone note",
			Marker:     "TODO",
			Properties: map[string]any{"duration": "30m"},
		},
		{
			UUID:    "t-note-uuid",
			Content: "TODO Think about the garden",
			Marker:  "TODO",
		},
	}
}

func TestForecastRefusesWithoutGoogleCredentials(t *testing.T) {
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})

	// Forecast reads the calendar (M4): a credentials file with no google
	// section is an operational refusal with the setup hint, and the
	// schedule it would print against a silently free calendar is withheld.
	code, out, errOut := e.Run("--json", "forecast")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 without the google section (stdout %q)", code, out)
	}
	if !strings.Contains(errOut, "google credentials: missing") || !strings.Contains(errOut, "http://127.0.0.1/") {
		t.Fatalf("stderr must carry the setup hint: %q", errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a refused forecast prints nothing: %q", out)
	}

	// The section present but the flow never run: the same refusal, pointing
	// at the one command that closes it.
	if _, err := writeCredentials(t, e.Dir,
		`{"logseq":{"token":"st-token"},"google":{"client_id":"cid","client_secret":"secret"}}`); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = e.Run("forecast")
	if code != 1 ||
		!strings.Contains(errOut, "refresh_token is empty") ||
		!strings.Contains(errOut, "lodo auth google") {
		t.Fatalf("exit = %d stderr = %q, want the auth hint", code, errOut)
	}
}

func TestCheckReportsAndStrictExitsTwo(t *testing.T) {
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})

	// Without --strict: issues are reported, exit stays 0.
	code, out, errOut := e.Run("check")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "todo-missing-duration") || !strings.Contains(out, "Think about the garden") {
		t.Fatalf("the TODO without duration must surface:\n%s", out)
	}

	// With --strict: the same report, exit 2, nothing written but the report.
	code, out, errOut = e.Run("check", "--strict")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 under --strict (stderr %q)", code, errOut)
	}
	if !strings.Contains(out, "todo-missing-duration") {
		t.Fatalf("the report must still print before the non-zero exit:\n%s", out)
	}
	if !strings.Contains(errOut, "lodo: check --strict:") {
		t.Fatalf("stderr must explain the validation exit: %q", errOut)
	}
}

func TestCheckJSONEndToEnd(t *testing.T) {
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})

	code, out, errOut := e.Run("--json", "check")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	var view struct {
		SchemaVersion int                     `json:"schema_version"`
		GraphIssues   []struct{ Kind string } `json:"graph_issues"`
		Shortfalls    []struct {
			TaskID    string `json:"task_id"`
			Shortfall int    `json:"shortfall"`
		} `json:"shortfalls"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, out)
	}
	if view.SchemaVersion != wire.CheckVersion {
		t.Fatalf("schema_version = %d, want %d", view.SchemaVersion, wire.CheckVersion)
	}
	if len(view.GraphIssues) != 1 || view.GraphIssues[0].Kind != "todo-missing-duration" {
		t.Fatalf("graph_issues = %+v", view.GraphIssues)
	}
	if len(view.Shortfalls) != 0 {
		t.Fatalf("shortfalls = %+v; the Task fits the default map", view.Shortfalls)
	}
}

func TestCheckOverridesAreErrorsAndMissingConfigOperational(t *testing.T) {
	// Missing config file: exit 1, with the path spelled out.
	missing := filepath.Join(t.TempDir(), "absent", "config.yaml")
	code, _, errOut := runCLI(t, missing, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a missing config", code)
	}
	if !strings.Contains(errOut, "config file") {
		t.Fatalf("stderr must explain the missing config: %q", errOut)
	}

	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})
	// Bad one-run overrides: still exit 1 (bad config).
	code, _, errOut = e.Run("forecast", "--window-days", "-3")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a negative override", code)
	}
	if !strings.Contains(errOut, "--window-days") {
		t.Fatalf("stderr must name the override: %q", errOut)
	}

	code, _, errOut = e.Run("forecast", "--timezone", "Mars/Olympus_Mons")
	if code != 1 || !strings.Contains(errOut, "--timezone") {
		t.Fatalf("exit = %d stderr = %q, want 1 naming the timezone override", code, errOut)
	}
}

func TestStubUnavailableTarget(t *testing.T) {
	// A credentials file with loose permissions is refused: exit 1.
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})
	if err := os.Chmod(e.Cred, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := e.Run("check")
	if code != 1 || !strings.Contains(errOut, "0600") {
		t.Fatalf("exit = %d stderr = %q, want 1 for loose credentials", code, errOut)
	}
}

func TestAuthGoogleRefusesWithoutSetup(t *testing.T) {
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})

	// No google section at all: exit 1 with the setup hint, and no
	// question asked of a user who cannot answer it.
	code, out, errOut := e.Run("auth", "google")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 without the google section", code)
	}
	if !strings.Contains(errOut, "google credentials: missing") ||
		!strings.Contains(errOut, "http://127.0.0.1/") {
		t.Fatalf("stderr must carry the setup hint: %q", errOut)
	}
	if strings.Contains(out, "Finish Google's authorization") {
		t.Fatalf("no URL may be raised when setup is missing: %q", out)
	}

	// A google section without the pair inside it: the same refusal, worded
	// about the pair this time.
	if _, err := writeCredentials(t, e.Dir, `{"google":{}}`); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = e.Run("auth", "google")
	if code != 1 || !strings.Contains(errOut, "client_id and client_secret are required") {
		t.Fatalf("exit = %d stderr = %q, want 1 naming the pair", code, errOut)
	}
}

func TestAuthGoogleHelpNamesTheFlow(t *testing.T) {
	e := mustE2E(t, e2eOptions{Tasks: missingDurationGraph()})
	code, out, errOut := e.Run("auth", "google", "--help")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"http://127.0.0.1/", "calendar.events", "--no-launch-browser", "Google Workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help is missing %q:\n%s", want, out)
		}
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// missingTimeMapsServer is the graph without the configured page: the stub
// records the bootstrap writes and serves the created page on later reads,
// so one run proves the whole offer-create-continue loop.
func missingTimeMapsServer(t *testing.T, writes *[]string) *httptest.Server {
	t.Helper()
	created := false
	dayTree := []*logseq.Block{
		{
			UUID:       "t-task-uuid",
			Content:    "TODO Write the milestone note",
			Marker:     "TODO",
			Properties: map[string]any{"duration": "30m"},
		},
	}
	tmTree := []*logseq.Block{
		{
			UUID:       "map-default-uuid",
			Content:    "Deep Work",
			Properties: map[string]any{"default": true},
			Children:   []*logseq.Block{{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"}},
		},
	}
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("bad stub request: %v", err)
			return
		}
		name := ""
		if len(req.Args) > 0 {
			name, _ = req.Args[0].(string)
		}
		var out string
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			if created {
				out = fmt.Sprintf(`[{"name":%q,"uuid":"d-uuid"},{"name":"Time Maps","uuid":"tm-uuid"}]`, e2eDay(0))
			} else {
				out = fmt.Sprintf(`[{"name":%q,"uuid":"d-uuid"}]`, e2eDay(0))
			}
		case "logseq.Editor.get_page_blocks_tree":
			if name == "Time Maps" && created {
				out = marshal(t, tmTree)
			} else {
				out = marshal(t, dayTree)
			}
		case "logseq.Editor.get_page":
			if name == "Time Maps" && !created {
				out = `null`
			} else {
				out = `{"name":"` + name + `","uuid":"x-uuid","properties":null}`
			}
		case "logseq.Editor.create_page":
			*writes = append(*writes, "create "+name)
			created = true
			out = `{"name":"time maps","uuid":"tm-uuid"}`
		case "logseq.Editor.append_block_in_page":
			*writes = append(*writes, "append "+name)
			out = `{"uuid":"b-new","content":"x"}`
		case "logseq.Editor.update_block":
			*writes = append(*writes, "update")
			out = `{}`
		case "logseq.Editor.insert_block":
			*writes = append(*writes, "insert")
			out = `{"uuid":"w-new","content":"x"}`
		case "logseq.Editor.upsert_block_property":
			*writes = append(*writes, "prop")
			out = `{}`
		default:
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, out)
	}))
	srv.Start()
	return srv
}

func TestCheckYesCreatesMissingTimeMapsPage(t *testing.T) {
	var writes []string
	srv := missingTimeMapsServer(t, &writes)

	dir := t.TempDir()
	conf, err := writeConfig(t, dir, srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeCredentials(t, dir, e2eLogseqCreds); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runCLI(t, conf, "check", "--yes")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if len(writes) == 0 || writes[0] != "create Time Maps" {
		t.Fatalf("writes = %v, want the page created first", writes)
	}
	if strings.Contains(out, "missing-time-maps-page") {
		t.Fatalf("the created page must resolve the issue:\n%s", out)
	}
	if !strings.Contains(errOut, "Created Time Maps page") {
		t.Fatalf("stderr must note the bootstrap: %q", errOut)
	}
}
