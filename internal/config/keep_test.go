package config_test

import (
	"os"
	"testing"

	"github.com/aries1980/lodo/internal/config"
)

const keepValid = `
logseq:
  time_maps_page: Time Maps
  graphs:
    - name: work
      api_url: http://127.0.0.1:12315/api
keep:
  sync_url: https://sync.example
  sources:
    - name: personal
      backend: keep-personal
      label: lodo
      default_time_map: deep-work
    - name: shared
      backend: keep-workspace
scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London
calendar:
  provider: google
`

func TestKeepSourcesLoad(t *testing.T) {
	cfg, err := config.Load(write(t, keepValid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Keep.Sources) != 2 {
		t.Fatalf("Sources = %+v, want 2", cfg.Keep.Sources)
	}
	if cfg.Keep.SyncURL != "https://sync.example" {
		t.Fatalf("SyncURL = %q", cfg.Keep.SyncURL)
	}
	if got := cfg.SourceDefault("personal"); got != "deep-work" {
		t.Fatalf("SourceDefault(personal) = %q", got)
	}
	if got := cfg.SourceOrder("work"); got != 0 {
		t.Fatalf("SourceOrder(work) = %d, want 0", got)
	}
	if got := cfg.SourceOrder("personal"); got != 1 {
		t.Fatalf("SourceOrder(personal) = %d, want 1 (after graphs)", got)
	}
	if got := cfg.SourceOrder("SHARED"); got != 2 {
		t.Fatalf("SourceOrder lookup must be case-insensitive, got %d", got)
	}
}

func TestKeepOnlyMode(t *testing.T) {
	body := `
keep:
  sources:
    - {name: personal, backend: keep-personal}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Keep-only mode must not require logseq.api_url: %v", err)
	}
	if len(cfg.Keep.Sources) != 1 {
		t.Fatalf("Sources = %+v", cfg.Keep.Sources)
	}
}

func TestKeepSourcesValidation(t *testing.T) {
	bodies := []struct {
		name   string
		body   string
		wantIn string
	}{
		{"missing name", `
keep:
  sources:
    - {backend: keep-personal}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "keep.sources[0].name"},
		{"missing backend", `
keep:
  sources:
    - {name: personal}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "keep.sources[0].backend"},
		{"bad backend", `
keep:
  sources:
    - {name: personal, backend: keep-official}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "keep.sources[0].backend"},
		{"clash with graph slug", `
logseq:
  graphs:
    - {name: work, api_url: http://127.0.0.1:12315/api}
keep:
  sources:
    - {name: Work, backend: keep-personal}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "duplicate source name"},
		{"bad sync url", `
keep:
  sync_url: sync.example/no-scheme
  sources:
    - {name: personal, backend: keep-personal}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`, "keep.sync_url"},
	}
	for _, tt := range bodies {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(write(t, tt.body))
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if !contains(err.Error(), tt.wantIn) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantIn)
			}
		})
	}
}

func TestTokenForKeepSource(t *testing.T) {
	t.Setenv("LODO_KEEP_TOKEN", "fallback")
	t.Setenv("LODO_KEEP_TOKEN_PERSONAL", " personal-tok ")
	c := config.Credentials{
		Keep: config.KeepCredentials{
			Token: "fallback",
			Sources: map[string]config.KeepSourceCredentials{
				"shared": {Token: "file-shared"},
			},
		},
	}
	if got := c.TokenForKeepSource("personal"); got != "personal-tok" {
		t.Fatalf("personal token = %q, want trimmed env", got)
	}
	if got := c.TokenForKeepSource("SHARED"); got != "file-shared" {
		t.Fatalf("shared token = %q, want file", got)
	}
	if got := c.TokenForKeepSource("other"); got != "fallback" {
		t.Fatalf("other token = %q, want global fallback", got)
	}
	if got := config.KeepTokenEnvVar("my-notes"); got != "LODO_KEEP_TOKEN_MY_NOTES" {
		t.Fatalf("env var = %q", got)
	}
	if c.HasAnyKeepToken() != true {
		t.Fatal("HasAnyKeepToken must be true")
	}
}

func TestHasAnyKeepTokenEmpty(t *testing.T) {
	for _, k := range []string{"LODO_KEEP_TOKEN", "LODO_KEEP_TOKEN_PERSONAL"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { t.Setenv(k, v) })
			if err := os.Unsetenv(k); err != nil {
				t.Fatal(err)
			}
		}
	}
	if (config.Credentials{}).HasAnyKeepToken() {
		t.Fatal("empty credentials must have no keep token")
	}
}
