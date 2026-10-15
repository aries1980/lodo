package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/config"
)

// valid is a config.yaml that passes validation, for tables to mutate.
const valid = `
logseq:
  api_url: http://127.0.0.1:12315/api
  time_maps_page: Time Maps

scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London

calendar:
  provider: google
  id: primary
`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantConfig() config.Config {
	return config.Config{
		Logseq: config.Logseq{
			APIURL:       "http://127.0.0.1:12315/api",
			TimeMapsPage: "Time Maps",
		},
		Scheduling: config.Scheduling{
			Granularity: "15m",
			WindowDays:  14,
			Timezone:    "Europe/London",
		},
		Calendar: config.Calendar{
			Provider: "google",
			ID:       "primary",
		},
	}
}

func TestLoadSpecExample(t *testing.T) {
	cfg, err := config.Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff(wantConfig(), cfg); diff != "" {
		t.Fatalf("Config mismatch (-want +got):\n%s", diff)
	}
}

func TestDefaultsApply(t *testing.T) {
	body := `
logseq:
  api_url: https://logseq.example/api
scheduling:
  granularity: 30m
  window_days: 21
  timezone: Pacific/Auckland
calendar:
  provider: google
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logseq.TimeMapsPage != "Time Maps" {
		t.Fatalf("TimeMapsPage = %q, want the default \"Time Maps\"", cfg.Logseq.TimeMapsPage)
	}
	if cfg.Calendar.ID != "primary" {
		t.Fatalf("Calendar.ID = %q, want the default \"primary\"", cfg.Calendar.ID)
	}
	if cfg.Logseq.Insecure {
		t.Fatal("Insecure must default to false")
	}
}

func TestInsecureApplies(t *testing.T) {
	body := `
logseq:
  api_url: https://logseq.example/api
  insecure: true
scheduling:
  granularity: 30m
  window_days: 21
  timezone: Pacific/Auckland
calendar:
  provider: google
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Logseq.Insecure {
		t.Fatal("logseq.insecure: true must reach the config")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantIn string
	}{
		{
			"not yaml at all",
			"\n- [unclosed: {",
			"config file",
		},
		{
			"wrong type",
			`logseq:
  api_url: [1, 2]
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"config file",
		},
		{
			"missing api_url",
			`logseq:
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"logseq.api_url: required",
		},
		{
			"relative api_url",
			`logseq:
  api_url: logseq.local/api
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"logseq.api_url",
		},
		{
			"25m does not divide 60",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 25m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.granularity \"25m\": must divide 60 minutes",
		},
		{
			"1h30m does not divide 60",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 1h30m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.granularity \"1h30m\": must divide 60 minutes",
		},
		{
			"90s is not whole minutes",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 90s
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.granularity \"90s\": must be a whole number of minutes",
		},
		{
			"missing granularity",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.granularity: required",
		},
		{
			"zero window days",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: 0
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.window_days: must be positive, got 0",
		},
		{
			"negative window days",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: -3
  timezone: Europe/London
calendar:
  provider: google
`,
			"scheduling.window_days: must be positive, got -3",
		},
		{
			"unknown timezone",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Mars/Olympus_Mons
calendar:
  provider: google
`,
			"scheduling.timezone",
		},
		{
			"missing timezone",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: 14
  timezone: ""
calendar:
  provider: google
`,
			"scheduling.timezone: required",
		},
		{
			"unknown provider",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: outlook
`,
			"calendar.provider \"outlook\": want \"google\" in v0.1",
		},
		{
			"missing provider",
			`logseq:
  api_url: http://127.0.0.1:12315/api
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  id: personal
`,
			"calendar.provider: required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := config.Load(write(t, tt.body)); err == nil {
				t.Fatalf("Load succeeded, want an error containing %q", tt.wantIn)
			} else if !strings.Contains(err.Error(), tt.wantIn) {
				t.Fatalf("Load error = %q, want it to contain %q", err.Error(), tt.wantIn)
			}
		})
	}
}

func TestParseGranularity(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr string
	}{
		{"15m", "15m", 15 * time.Minute, ""},
		{"20m", "20m", 20 * time.Minute, ""},
		{"30m", "30m", 30 * time.Minute, ""},
		{"5m", "5m", 5 * time.Minute, ""},
		{"1h", "1h", time.Hour, ""},
		{"padded", " 15m ", 15 * time.Minute, ""},
		{"25m does not divide 60", "25m", 0, "must divide 60 minutes"},
		{"90m does not divide 60", "90m", 0, "must divide 60 minutes"},
		{"45m does not divide 60", "45m", 0, "must divide 60 minutes"},
		{"90s is not whole minutes", "90s", 0, "whole number of minutes"},
		{"less than a minute", "500ms", 0, "whole number of minutes"},
		{"zero", "0m", 0, "must be positive"},
		{"no unit", "15", 0, "parse duration"},
		{"garbage", "quarter", 0, "parse duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.ParseGranularity(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseGranularity(%q): %v", tt.in, err)
				}
				if got != tt.want {
					t.Fatalf("ParseGranularity(%q) = %v, want %v", tt.in, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseGranularity(%q) = %v, want error containing %q", tt.in, got, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseGranularity(%q) error = %q, want it to contain %q", tt.in, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLocation(t *testing.T) {
	s := config.Scheduling{Timezone: "Europe/London"}
	loc, err := s.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if got := loc.String(); got != "Europe/London" {
		t.Fatalf("location name = %q, want \"Europe/London\"", got)
	}

	s = config.Scheduling{Timezone: "Not/AZone"}
	if _, err := s.Location(); err == nil {
		t.Fatal("Location succeeded for an unknown zone")
	}
}

func TestPathMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	if _, err := config.Load(missing); err == nil {
		t.Fatal("Load succeeded for a missing file; forecast/schedule must refuse without config")
	}
}

func TestOverrides(t *testing.T) {
	cfg := wantConfig()

	// No overrides: the config is returned unchanged.
	got, err := config.Apply(cfg, config.Overrides{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if diff := cmp.Diff(cfg, got); diff != "" {
		t.Fatalf("no overrides changed the config (-want +got):\n%s", diff)
	}

	// Overrides take precedence for one run...
	got, err = config.Apply(cfg, config.Overrides{WindowDays: 30, Timezone: "America/New_York"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Scheduling.WindowDays != 30 {
		t.Fatalf("WindowDays = %d, want the override 30", got.Scheduling.WindowDays)
	}
	if got.Scheduling.Timezone != "America/New_York" {
		t.Fatalf("Timezone = %q, want the override", got.Scheduling.Timezone)
	}
	if got.Logseq.APIURL != cfg.Logseq.APIURL {
		t.Fatal("an override touched something it does not target")
	}

	// ...but the original is never mutated, and the override is not persisted:
	// the source Config still says 14.
	if cfg.Scheduling.WindowDays != 14 {
		t.Fatalf("Apply mutated the caller's config: WindowDays = %d", cfg.Scheduling.WindowDays)
	}

	// Timezone parses cleanly via Location().
	if _, err := got.Scheduling.Location(); err != nil {
		t.Fatalf("Location after override: %v", err)
	}
}

func TestOverrideErrors(t *testing.T) {
	cfg := wantConfig()
	if _, err := config.Apply(cfg, config.Overrides{WindowDays: -2}); err == nil {
		t.Fatal("Apply accepted a negative window-days override")
	}
	if _, err := config.Apply(cfg, config.Overrides{Timezone: "Mars/Olympus_Mons"}); err == nil {
		t.Fatal("Apply accepted an unknown timezone override")
	}
}
