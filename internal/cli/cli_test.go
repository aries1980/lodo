package cli_test

import (
	"context"
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

	"github.com/aries1980/lodo/internal/cli"
	"github.com/aries1980/lodo/internal/logseq"
)

// stubApp answers the API calls the read side makes, with a tiny graph: one
// Time Maps page with a default map, one journal page with a Task and one
// flagged TODO.
func stubApp(t *testing.T) *httptest.Server {
	t.Helper()
	dayTree := []*logseq.Block{
		{
			UUID:    "t-task-uuid",
			Content: "TODO Write the milestone note",
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
	tmTree := []*logseq.Block{
		{
			UUID:    "map-default-uuid",
			Content: "Deep Work",
			Marker:  "",
			Properties: map[string]any{
				"default": true,
			},
			Children: []*logseq.Block{
				{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("bad stub request: %v", err)
		}
		var out string
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			out = `[{"name":"2026-10-15","uuid":"d-uuid"},{"name":"Time Maps","uuid":"tm-uuid"}]`
		case "logseq.Editor.get_page_blocks_tree":
			if req.Args[0] == "Time Maps" {
				out = marshal(t, tmTree)
			} else {
				out = marshal(t, dayTree)
			}
		case "logseq.Editor.get_page":
			out = `{"name":"` + req.Args[0].(string) + `","uuid":"x-uuid","properties":null}`
		default:
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// testSetup writes a config file and a credentials file pointing at the stub
// app, and env-pointing the XDG defaults at temp dirs.
func testSetup(t *testing.T, srv *httptest.Server) (confPath, credPath string) {
	t.Helper()
	confPath = filepath.Join(t.TempDir(), "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
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
`, srv.URL)
	if err := os.WriteFile(confPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	credPath = filepath.Join(t.TempDir(), "state", "lodo", "credentials.json")
	credDir := filepath.Dir(credPath)
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte(`{"logseq":{"token":"st-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return confPath, credPath
}

// runCLI runs lodo with the given args plus XDG envs pointing at the test
// paths, returning the exit code and both streams.
func runCLI(t *testing.T, conf, cred string, args ...string) (int, string, string) {
	t.Helper()
	withEnv(t, "XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(conf)))
	withEnv(t, "XDG_STATE_HOME", filepath.Dir(filepath.Dir(cred)))

	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := cli.Run(append([]string{"--config", conf}, args...), stdout, stderr)
	return code, stdout.String(), stderr.String()
}

func withEnv(t *testing.T, key, val string) {
	t.Helper()
	t.Setenv(key, val)
}

type forecastView struct {
	Blocks []struct {
		TaskID string    `json:"task_id"`
		Task   string    `json:"task"`
		Index  int       `json:"index"`
		Start  time.Time `json:"start"`
		End    time.Time `json:"end"`
		Slots  int       `json:"slots"`
		Kind   string    `json:"kind"`
	} `json:"blocks"`
	Shortfalls []struct {
		TaskID    string `json:"task_id"`
		Shortfall int    `json:"shortfall"`
	} `json:"shortfalls"`
	GraphIssues []struct{ Kind string } `json:"graph_issues"`
}

func TestForecastEndToEnd(t *testing.T) {
	conf, cred := testSetup(t, stubApp(t))
	ctx := context.Background()
	_ = ctx

	code, out, errOut := runCLI(t, conf, cred, "--json", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q, stdout = %s", code, errOut, out)
	}

	var view forecastView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, out)
	}
	if len(view.Blocks) != 1 {
		t.Fatalf("blocks = %+v, want exactly one: the 30m Task inside the default map", view.Blocks)
	}
	b := view.Blocks[0]
	if b.TaskID != "t-task-uuid" || b.Kind != "flexible" || b.Slots != 2 {
		t.Fatalf("block = %+v, want the 30m flexible Task", b)
	}
	if len(view.Shortfalls) != 0 || len(view.GraphIssues) != 0 {
		t.Fatalf("unexpected report content: %+v", view)
	}
}

func TestForecastTextEndToEnd(t *testing.T) {
	conf, cred := testSetup(t, stubApp(t))

	code, out, errOut := runCLI(t, conf, cred, "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{
		`Schedule that would be written to calendar "primary"`,
		"30m  Write the milestone note",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
	// Explanations only with --verbose:
	if strings.Contains(out, "Explanations:") {
		t.Fatal("plain forecast must not include explanations")
	}

	code, out, errOut = runCLI(t, conf, cred, "--verbose", "forecast")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Explanations:") {
		t.Fatalf("--verbose must add explanations:\n%s", out)
	}
}

func TestCheckReportsAndStrictExitsTwo(t *testing.T) {
	conf, cred := testSetup(t, stubApp(t))

	// Without --strict: issues are reported, exit stays 0.
	code, out, errOut := runCLI(t, conf, cred, "check")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "todo-missing-duration") || !strings.Contains(out, "Think about the garden") {
		t.Fatalf("the TODO without duration must surface:\n%s", out)
	}

	// With --strict: the same report, exit 2, nothing written but the report.
	code, out, errOut = runCLI(t, conf, cred, "check", "--strict")
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
	conf, cred := testSetup(t, stubApp(t))

	code, out, errOut := runCLI(t, conf, cred, "--json", "check")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	var view struct {
		GraphIssues []struct{ Kind string } `json:"graph_issues"`
		Shortfalls  []struct {
			TaskID    string `json:"task_id"`
			Shortfall int    `json:"shortfall"`
		} `json:"shortfalls"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not the JSON contract: %v\n%s", err, out)
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
	code, _, errOut := runCLI(t, missing, t.TempDir(), "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a missing config", code)
	}
	if !strings.Contains(errOut, "config file") {
		t.Fatalf("stderr must explain the missing config: %q", errOut)
	}

	conf, cred := testSetup(t, stubApp(t))
	// Bad one-run overrides: still exit 1 (bad config).
	code, _, errOut = runCLI(t, conf, cred, "forecast", "--window-days", "-3")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a negative override", code)
	}
	if !strings.Contains(errOut, "--window-days") {
		t.Fatalf("stderr must name the override: %q", errOut)
	}

	code, _, errOut = runCLI(t, conf, cred, "forecast", "--timezone", "Mars/Olympus_Mons")
	if code != 1 || !strings.Contains(errOut, "--timezone") {
		t.Fatalf("exit = %d stderr = %q, want 1 naming the timezone override", code, errOut)
	}
}

func TestStubUnavailableTarget(t *testing.T) {
	// A credentials file with loose permissions is refused: exit 1.
	conf, cred := testSetup(t, stubApp(t))
	if err := os.Chmod(cred, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, conf, cred, "check")
	if code != 1 || !strings.Contains(errOut, "0600") {
		t.Fatalf("exit = %d stderr = %q, want 1 for loose credentials", code, errOut)
	}
}

func TestAuthGoogleRefusesWithoutSetup(t *testing.T) {
	conf, cred := testSetup(t, stubApp(t))

	// No google section at all: exit 1 with the setup hint, and no
	// question asked of a user who cannot answer it.
	code, out, errOut := runCLI(t, conf, cred, "auth", "google")
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
	if err := os.WriteFile(cred, []byte(`{"google":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = runCLI(t, conf, cred, "auth", "google")
	if code != 1 || !strings.Contains(errOut, "client_id and client_secret are required") {
		t.Fatalf("exit = %d stderr = %q, want 1 naming the pair", code, errOut)
	}
}

func TestAuthGoogleHelpNamesTheFlow(t *testing.T) {
	conf, cred := testSetup(t, stubApp(t))
	code, out, errOut := runCLI(t, conf, cred, "auth", "google", "--help")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	for _, want := range []string{"http://127.0.0.1/", "calendar.events", "--no-launch-browser", "Google Workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help is missing %q:\n%s", want, out)
		}
	}
}
