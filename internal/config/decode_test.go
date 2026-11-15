package config_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/config"
)

// documented is a config.yaml with every key configuration.md documents, so a
// yaml tag that no longer matches its key leaves a field zero here.
const documented = `
logseq:
  api_url: http://127.0.0.1:12315/api
  time_maps_page: Time Maps
  insecure: true
  graphs:
    - name: work
      api_url: http://127.0.0.1:12316/api
      time_maps_page: Work Maps
      default_time_map: deep-work
scheduling:
  granularity: 20m
  window_days: 30
  timezone: Pacific/Auckland
calendar:
  provider: google
  id: team@example.com
  read_calendars:
    - partner@example.com
  ignore_read_all_day: true
  ignore_read_all_day_matching:
    - Birthday
ui:
  theme: monokai
`

func TestDocumentedKeysDecode(t *testing.T) {
	cfg, err := config.Load(write(t, documented))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.Config{
		Logseq: config.Logseq{
			APIURL:       "http://127.0.0.1:12315/api",
			TimeMapsPage: "Time Maps",
			Insecure:     true,
			Graphs: []config.GraphSource{{
				Name:           "work",
				APIURL:         "http://127.0.0.1:12316/api",
				TimeMapsPage:   "Work Maps",
				DefaultTimeMap: "deep-work",
			}},
		},
		Scheduling: config.Scheduling{
			Granularity: "20m",
			WindowDays:  30,
			Timezone:    "Pacific/Auckland",
		},
		Calendar: config.Calendar{
			Provider:                 "google",
			ID:                       "team@example.com",
			ReadCalendars:            []string{"partner@example.com"},
			IgnoreReadAllDay:         true,
			IgnoreReadAllDayMatching: []string{"birthday"},
		},
		UI: config.UI{Theme: "monokai"},
	}
	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Fatalf("Config mismatch (-want +got):\n%s", diff)
	}
}

func TestStringsAreTrimmed(t *testing.T) {
	body := `
logseq:
  api_url: "  http://127.0.0.1:12315/api  "
  time_maps_page: "  Time Maps  "
  graphs:
    - name: "  work  "
      api_url: "  http://127.0.0.1:12316/api  "
      time_maps_page: "  Work Maps  "
      default_time_map: "  deep-work  "
scheduling:
  granularity: "  20m  "
  window_days: 30
  timezone: "  Pacific/Auckland  "
calendar:
  provider: "  google  "
  id: "  team@example.com  "
ui:
  theme: "  monokai  "
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.Config{
		Logseq: config.Logseq{
			APIURL:       "http://127.0.0.1:12315/api",
			TimeMapsPage: "Time Maps",
			Graphs: []config.GraphSource{{
				Name:           "work",
				APIURL:         "http://127.0.0.1:12316/api",
				TimeMapsPage:   "Work Maps",
				DefaultTimeMap: "deep-work",
			}},
		},
		Scheduling: config.Scheduling{
			Granularity: "20m",
			WindowDays:  30,
			Timezone:    "Pacific/Auckland",
		},
		Calendar: config.Calendar{
			Provider: "google",
			ID:       "team@example.com",
		},
		UI: config.UI{Theme: "monokai"},
	}
	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Fatalf("Config mismatch (-want +got):\n%s", diff)
	}
}

func TestWhitespaceOnlyValues(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantIn string
	}{
		{
			"api_url",
			`logseq: {api_url: "   "}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`,
			"logseq.api_url: required",
		},
		{
			"granularity",
			`logseq: {api_url: http://127.0.0.1:12315/api}
scheduling: {granularity: "   ", window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`,
			"scheduling.granularity: required",
		},
		{
			"timezone",
			`logseq: {api_url: http://127.0.0.1:12315/api}
scheduling: {granularity: 15m, window_days: 14, timezone: "   "}
calendar: {provider: google}
`,
			"scheduling.timezone: required",
		},
		{
			"provider",
			`logseq: {api_url: http://127.0.0.1:12315/api}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: "   "}
`,
			"calendar.provider: required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(write(t, tt.body))
			if err == nil {
				t.Fatalf("Load succeeded, want an error containing %q", tt.wantIn)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Fatalf("Load error = %q, want it to contain %q", err.Error(), tt.wantIn)
			}
		})
	}

	body := `logseq: {api_url: http://127.0.0.1:12315/api, time_maps_page: "   "}
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Logseq.TimeMapsPage != config.DefaultTimeMapsPage {
		t.Fatalf("TimeMapsPage = %q, want the default %q", cfg.Logseq.TimeMapsPage, config.DefaultTimeMapsPage)
	}
}

func TestEmptyListsMatchAbsent(t *testing.T) {
	body := `
logseq:
  api_url: http://127.0.0.1:12315/api
  graphs: []
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar:
  provider: google
  read_calendars: []
  ignore_read_all_day_matching: []
`
	cfg, err := config.Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Logseq.Graphs) != 0 {
		t.Fatalf("Graphs = %+v, want single-graph mode", cfg.Logseq.Graphs)
	}
	if got := cfg.Logseq.PrimaryTimeMapsPage(); got != config.DefaultTimeMapsPage {
		t.Fatalf("PrimaryTimeMapsPage = %q, want the global default", got)
	}
	if len(cfg.Calendar.ReadCalendars) != 0 {
		t.Fatalf("ReadCalendars = %q, want the write calendar alone", cfg.Calendar.ReadCalendars)
	}
	if cfg.Calendar.IgnoreReadAllDayMatching != nil {
		t.Fatalf("IgnoreReadAllDayMatching = %q, want nil", cfg.Calendar.IgnoreReadAllDayMatching)
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	// KnownFields is deliberately off: Load has always ignored unknown keys,
	// and a strictness policy would be its own decision.
	body := `
logseq:
  api_url: http://127.0.0.1:12315/api
  typo: ignored
scheduling: {granularity: 15m, window_days: 14, timezone: Europe/London}
calendar: {provider: google}
unknown_section:
  anything: 1
`
	if _, err := config.Load(write(t, body)); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestDuplicateKeysRejected(t *testing.T) {
	body := `
logseq: {api_url: http://127.0.0.1:12315/api}
scheduling:
  granularity: 15m
  window_days: 14
  window_days: 21
  timezone: Europe/London
calendar: {provider: google}
`
	_, err := config.Load(write(t, body))
	if err == nil {
		t.Fatal("Load accepted a duplicate key")
	}
	if !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("Load error = %q, want the yaml duplicate-key error", err.Error())
	}
}

func TestEmptyFileReportsConfigPath(t *testing.T) {
	if _, err := config.Load(write(t, "")); err == nil || !strings.Contains(err.Error(), "config file") {
		t.Fatalf("Load(empty) = %v, want a config file error", err)
	}
}

// yamlTagProblems reports every field the decoder would silently ignore: a
// missing tag, a duplicate tag within one struct, or a section that is not a
// struct. An explicit yaml:"-" opts a field and its nested types out.
func yamlTagProblems(root reflect.Type) []string {
	var problems []string
	seen := map[reflect.Type]bool{}

	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		if typ.Kind() != reflect.Struct {
			problems = append(problems, fmt.Sprintf("%s: want a struct", typ))
			return
		}
		tags := map[string]string{}
		for f := range typ.Fields() {
			tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			switch tag {
			case "":
				problems = append(problems, fmt.Sprintf("%s.%s: missing yaml tag", typ.Name(), f.Name))
				continue
			case "-":
				continue
			}
			if prev, ok := tags[tag]; ok {
				problems = append(problems, fmt.Sprintf("%s: yaml tag %q is used by both %s and %s", typ.Name(), tag, prev, f.Name))
			}
			tags[tag] = f.Name

			ft := f.Type
			for ft.Kind() == reflect.Slice || ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				walk(ft)
			}
		}
	}

	walk(root)
	return problems
}

func TestConfigSectionsHaveYAMLTags(t *testing.T) {
	if problems := yamlTagProblems(reflect.TypeOf(config.Config{})); len(problems) > 0 {
		t.Fatalf("yaml tags:\n%s", strings.Join(problems, "\n"))
	}
}

func TestYAMLTagsAllowExplicitExclusion(t *testing.T) {
	type unkeyed struct {
		Value string
	}
	type fixture struct {
		Keyed    string  `yaml:"keyed"`
		Excluded unkeyed `yaml:"-"`
	}
	if problems := yamlTagProblems(reflect.TypeOf(fixture{})); len(problems) > 0 {
		t.Fatalf("yaml tags:\n%s", strings.Join(problems, "\n"))
	}
}

func TestYAMLTagsRejectBadFields(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
	}{
		{"missing tag", reflect.TypeOf(struct{ Value string }{})},
		{"duplicate tag", reflect.TypeOf(struct {
			One string `yaml:"value"`
			Two string `yaml:"value"`
		}{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if problems := yamlTagProblems(tt.typ); len(problems) == 0 {
				t.Fatalf("yamlTagProblems(%s) accepted the fixture", tt.typ)
			}
		})
	}
}
