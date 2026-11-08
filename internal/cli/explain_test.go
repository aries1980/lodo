package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/oauth2"
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
