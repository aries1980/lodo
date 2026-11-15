package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/store"
)

func TestExplainTextShowsFullReasoning(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "explain", "milestone"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Explain:",
		"Eligibility:",
		"granularity:",
		"Effective properties",
		"Dispositions",
		"Ordering / tie-breaks",
		"tie-break:",
		"consideration:",
		"Dependency gate:",
		"Plan / window:",
		"Time Maps / tiers",
		"rejected_busy",
		"comparator (ADR 0010)",
		"Final blocks / shortfall:",
		"outcome:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("explain text missing %q:\n%s", want, out)
		}
	}
}

func TestExplainJSONSchemaV2(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &bytes.Buffer{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "explain", "t-task-uuid"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	var view struct {
		SchemaVersion int `json:"schema_version"`
		Ordering      struct {
			Kind           string `json:"kind"`
			TieBreak       string `json:"tie_break"`
			TieBreakDetail string `json:"tie_break_detail"`
			Position       int    `json:"position"`
			Total          int    `json:"total"`
		} `json:"ordering"`
		Eligibility struct {
			Considered bool `json:"considered"`
		} `json:"eligibility"`
		TimeMaps struct {
			Passes []struct {
				Tier                string `json:"tier"`
				RejectedBusy        int    `json:"rejected_busy"`
				RejectedExternal    int    `json:"rejected_external"`
				RejectedOutOfWindow int    `json:"rejected_out_of_window"`
			} `json:"passes"`
		} `json:"time_maps"`
		Outcome struct {
			Required  int `json:"required"`
			Placed    int `json:"placed"`
			Shortfall int `json:"shortfall"`
		} `json:"outcome"`
		Spillover *struct {
			Summary string `json:"summary"`
		} `json:"spillover"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not explain JSON: %v\n%s", err, stdout.String())
	}
	if view.SchemaVersion != 2 {
		t.Fatalf("schema_version=%d, want 2", view.SchemaVersion)
	}
	if view.Ordering.TieBreak == "" || view.Ordering.TieBreakDetail == "" {
		t.Fatalf("ordering tie-break missing: %+v", view.Ordering)
	}
	if view.Ordering.Position == 0 || view.Ordering.Total == 0 {
		t.Fatalf("ordering position missing: %+v", view.Ordering)
	}
	if len(view.TimeMaps.Passes) == 0 {
		t.Fatalf("time_maps passes missing")
	}
	if view.Outcome.Required == 0 || view.Outcome.Placed == 0 {
		t.Fatalf("outcome missing: %+v", view.Outcome)
	}
}

func TestExplainNoMatchJSON(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout := &bytes.Buffer{}
	stderr := &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "explain", "no-such-task"}, stdout, stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var view struct {
		SchemaVersion int    `json:"schema_version"`
		Error         string `json:"error"`
		Query         string `json:"query"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not error JSON: %v\n%s", err, stdout.String())
	}
	if view.SchemaVersion != explainSchemaVersion {
		t.Fatalf("schema_version=%d", view.SchemaVersion)
	}
	if view.Error == "" || view.Query != "no-such-task" {
		t.Fatalf("error view: %+v", view)
	}
}

func TestExplainReportDispositionsAreTyped(t *testing.T) {
	tasks := []model.Task{{ID: "task-1", Text: "Write the note", Graph: "work"}}
	start := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	view := explainReport{
		SchemaVersion: explainSchemaVersion,
		Dispositions: dispositionViews(tasks, []model.Disposition{{
			TaskID: "task-1", BlockIndex: 1, Kind: model.DispositionPinned,
			Start: start, End: start.Add(30 * time.Minute), Slots: 2,
			Snapshot: model.Snapshot{Text: "Write the note", Duration: 90 * time.Minute},
		}}),
	}
	raw, err := jsonx.Marshal(view)
	if err != nil {
		t.Fatalf("marshal explain report: %v", err)
	}
	for _, want := range []string{
		`"task_id":"task-1"`, `"task":"Write the note"`, `"graph":"work"`,
		`"block_index":1`, `"kind":"pinned"`, `"slots":2`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("payload missing %s:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "Snapshot") || strings.Contains(string(raw), "duration_ns") {
		t.Fatalf("domain snapshot leaked into the payload:\n%s", raw)
	}
}

func seedExplainDisposition(t *testing.T, credPath string, d model.Disposition) {
	t.Helper()
	st, err := store.Open(filepath.Join(filepath.Dir(credPath), "lodo.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Commit(nil, nil, []store.TimedDisposition{{Disposition: d, RecordedAt: time.Now()}}, nil); err != nil {
		t.Fatalf("seed disposition: %v", err)
	}
}

func TestExplainJSONPinnedDisposition(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, cred := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(15 * time.Minute)
	end := start.Add(30 * time.Minute)
	seedExplainDisposition(t, cred, model.Disposition{
		TaskID: "t-task-uuid", BlockIndex: 0, Kind: model.DispositionPinned,
		Start: start, End: end, Slots: 2,
	})

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "explain", "t-task-uuid"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	var view struct {
		SchemaVersion int `json:"schema_version"`
		Dispositions  []struct {
			TaskID     string    `json:"task_id"`
			Task       string    `json:"task"`
			Graph      string    `json:"graph"`
			BlockIndex int       `json:"block_index"`
			Kind       string    `json:"kind"`
			Start      time.Time `json:"start"`
			End        time.Time `json:"end"`
			Slots      int       `json:"slots"`
		} `json:"dispositions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not explain JSON: %v\n%s", err, stdout.String())
	}
	if view.SchemaVersion != explainSchemaVersion {
		t.Fatalf("schema_version = %d", view.SchemaVersion)
	}
	if len(view.Dispositions) != 1 {
		t.Fatalf("dispositions = %+v", view.Dispositions)
	}
	d := view.Dispositions[0]
	if d.TaskID != "t-task-uuid" || d.Task != "Write the milestone note" || d.BlockIndex != 0 || d.Kind != "pinned" || d.Slots != 2 {
		t.Fatalf("disposition = %+v", d)
	}
	if !d.Start.Equal(start) || !d.End.Equal(end) {
		t.Fatalf("span = %v..%v, want %v..%v", d.Start, d.End, start, end)
	}
	if strings.Contains(stdout.String(), "Snapshot") {
		t.Fatalf("domain snapshot leaked:\n%s", stdout.String())
	}
}

func TestExplainJSONStoodDownDisposition(t *testing.T) {
	f := &scheduleFake{}
	api := miniApp(t)
	conf, cred := writeTestSetup(t, api)
	base := f.mount(t, nil)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	// One schedule seeds the store with the Task's resolved snapshot; a
	// stood-down disposition only applies while that snapshot matches.
	if code := Run([]string{"--config", conf, "--json", "schedule"}, &bytes.Buffer{}, &strings.Builder{}); code != 0 {
		t.Fatalf("seed schedule exit = %d", code)
	}
	storePath := filepath.Join(filepath.Dir(cred), "lodo.db")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	snapshots, err := st.Snapshots()
	_ = st.Close()
	if err != nil {
		t.Fatalf("snapshots: %v", err)
	}
	snap, ok := snapshots["t-task-uuid"]
	if !ok {
		t.Fatalf("no snapshot for t-task-uuid: %+v", snapshots)
	}

	seedExplainDisposition(t, cred, model.Disposition{
		TaskID: "t-task-uuid", BlockIndex: 0, Kind: model.DispositionStoodDown,
		Slots: 2, Snapshot: snap,
	})

	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--json", "explain", "t-task-uuid"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	var view struct {
		SchemaVersion int `json:"schema_version"`
		Dispositions  []struct {
			TaskID     string `json:"task_id"`
			BlockIndex int    `json:"block_index"`
			Kind       string `json:"kind"`
			Slots      int    `json:"slots"`
		} `json:"dispositions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("stdout is not explain JSON: %v\n%s", err, stdout.String())
	}
	if len(view.Dispositions) != 1 {
		t.Fatalf("dispositions = %+v", view.Dispositions)
	}
	if d := view.Dispositions[0]; d.TaskID != "t-task-uuid" || d.BlockIndex != 0 || d.Kind != "stood-down" || d.Slots != 2 {
		t.Fatalf("disposition = %+v", d)
	}
	if strings.Contains(stdout.String(), "Snapshot") {
		t.Fatalf("domain snapshot leaked:\n%s", stdout.String())
	}
}

func TestExplainVerboseAddsConsiderationOrder(t *testing.T) {
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	plain := &strings.Builder{}
	if code := Run([]string{"--config", conf, "explain", "milestone"}, plain, &strings.Builder{}); code != 0 {
		t.Fatalf("plain exit")
	}
	if strings.Contains(plain.String(), "Consideration order (--verbose)") {
		t.Fatalf("plain explain must not list consideration order")
	}
	verbose := &strings.Builder{}
	if code := Run([]string{"--config", conf, "--verbose", "explain", "milestone"}, verbose, &strings.Builder{}); code != 0 {
		t.Fatalf("verbose exit")
	}
	if !strings.Contains(verbose.String(), "Consideration order (--verbose)") {
		t.Fatalf("verbose explain must list consideration order:\n%s", verbose.String())
	}
}
