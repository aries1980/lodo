package config_test

import (
	"testing"

	"github.com/aries1980/lodo/internal/config"
)

const multiValid = `
logseq:
  time_maps_page: Time Maps
  graphs:
    - name: work
      api_url: http://127.0.0.1:12315/api
      default_time_map: deep-work
    - name: family
      api_url: http://127.0.0.1:12316/api
      default_time_map: weekends
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`

func TestGraphsLoad(t *testing.T) {
	cfg, err := config.Load(write(t, multiValid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Logseq.Graphs) != 2 {
		t.Fatalf("Graphs = %+v, want 2", cfg.Logseq.Graphs)
	}
	if cfg.Logseq.Graphs[0].Name != "work" || cfg.Logseq.Graphs[0].DefaultTimeMap != "deep-work" {
		t.Fatalf("graphs[0] = %+v", cfg.Logseq.Graphs[0])
	}
	if got := cfg.Logseq.PrimaryTimeMapsPage(); got != "Time Maps" {
		t.Fatalf("PrimaryTimeMapsPage = %q", got)
	}
	if got := cfg.Logseq.GraphDefault("work"); got != "deep-work" {
		t.Fatalf("GraphDefault(work) = %q", got)
	}
	if got := cfg.Logseq.GraphOrder("family"); got != 1 {
		t.Fatalf("GraphOrder(family) = %d, want 1", got)
	}
	if got := cfg.Logseq.GraphOrder("WORK"); got != 0 {
		t.Fatalf("GraphOrder lookup must be case-insensitive, got %d", got)
	}
}

func TestGraphsValidation(t *testing.T) {
	bodies := []struct {
		name   string
		body   string
		wantIn string
	}{
		{"missing name", `
logseq:
  graphs:
    - api_url: http://127.0.0.1:12315/api
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "logseq.graphs[0].name"},
		{"duplicate slug", `
logseq:
  graphs:
    - {name: Work, api_url: http://127.0.0.1:12315/api}
    - {name: work, api_url: http://127.0.0.1:12316/api}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "duplicate graph name"},
		{"bad url", `
logseq:
  graphs:
    - {name: work, api_url: logseq.local/api}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "must be an absolute http"},
		{"bad default map", `
logseq:
  graphs:
    - {name: work, api_url: http://127.0.0.1:12315/api, default_time_map: " *** "}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "default_time_map"},
	}
	for _, tt := range bodies {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := config.Load(write(t, tt.body)); err == nil {
				t.Fatal("Load succeeded, want error")
			}
		})
		// check substring via second load for message
		_, err := config.Load(write(t, tt.body))
		if err == nil {
			return
		}
		found := false
		for _, s := range []string{tt.wantIn} {
			if len(s) > 0 && contains(err.Error(), s) {
				found = true
			}
		}
		if !found {
			t.Fatalf("error %q does not contain %q", err.Error(), tt.wantIn)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestTokenForGraph(t *testing.T) {
	t.Setenv("LODO_LOGSEQ_TOKEN", "fallback")
	t.Setenv("LODO_LOGSEQ_TOKEN_WORK", " work-tok ")
	// Global Token already carries the LODO_LOGSEQ_TOKEN overlay, the way
	// ReadCredentials.withEnv leaves it before TokenForGraph runs.
	c := config.Credentials{
		Logseq: config.LogseqCredentials{
			Token: "fallback",
			Graphs: map[string]config.LogseqGraphCredentials{
				"family": {Token: "file-family"},
			},
		},
	}
	if got := c.TokenForGraph("work"); got != "work-tok" {
		t.Fatalf("work token = %q, want trimmed env", got)
	}
	if got := c.TokenForGraph("FAMILY"); got != "file-family" {
		t.Fatalf("family token = %q, want file", got)
	}
	if got := c.TokenForGraph("personal"); got != "fallback" {
		t.Fatalf("personal token = %q, want global fallback", got)
	}
	if got := config.GraphTokenEnvVar("my-work"); got != "LODO_LOGSEQ_TOKEN_MY_WORK" {
		t.Fatalf("env var = %q", got)
	}
}
