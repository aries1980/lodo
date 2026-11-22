// Package config loads Lodo's configuration and credentials (ADR 0005): the
// graph holds user intent about work, the config file holds how Lodo runs,
// and everything secret lives in the state directory or the environment.
//
// The deciding rule for any future setting: secret or machine-bound → file
// (or environment for containers); user intent about work → graph.
package config

import (
	"bytes"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/keep"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/parse"
	"github.com/aries1980/lodo/internal/set"
	"github.com/aries1980/lodo/internal/theme"
)

// Config mirrors config.yaml; it decodes from the file directly and validate
// owns trimming, defaults and errors.
type Config struct {
	Logseq     Logseq     `json:"logseq" yaml:"logseq"`
	Keep       Keep       `json:"keep" yaml:"keep"`
	Scheduling Scheduling `json:"scheduling" yaml:"scheduling"`
	Calendar   Calendar   `json:"calendar" yaml:"calendar"`
	UI         UI         `json:"ui" yaml:"ui"`
}

// UI holds human-output presentation: the named color scheme.
type UI struct {
	Theme string `json:"theme" yaml:"theme"`
}

// Logseq is the graph source: where Lodo reads over the HTTP API (ADR 0002).
type Logseq struct {
	// APIURL is the Logseq app's HTTP API endpoint, e.g.
	// http://127.0.0.1:12315/api.
	APIURL string `yaml:"api_url"`
	// TimeMapsPage names the page holding the Time Maps; default "Time Maps".
	TimeMapsPage string `yaml:"time_maps_page"`
	// Insecure skips TLS verification for an app serving a self-signed
	// certificate. It is machine-bound, so it lives in the file (ADR 0005),
	// never as a flag: whoever wrote the config accepted that certificate.
	Insecure bool `yaml:"insecure"`
	// Graphs lists the Logseq graphs to merge, in priority order. Empty
	// means single-graph mode (APIURL alone). When non-empty, each entry
	// needs its own Logseq window on its own HTTP port: one endpoint
	// always serves its current graph only.
	Graphs []GraphSource `yaml:"graphs"`
}

// GraphSource is one Logseq graph in a multi-graph setup.
type GraphSource struct {
	// Name is the graph slug (work, family, personal). It fills
	// Task.Graph, check/forecast output and the calendar graph field.
	Name string `yaml:"name"`
	// APIURL is this graph HTTP API endpoint.
	APIURL string `yaml:"api_url"`
	// TimeMapsPage optionally overrides the canonical page for this graph;
	// empty means the global TimeMapsPage. Canonical Time Maps are read
	// from the first graph; this field is reserved for a future union.
	TimeMapsPage string `yaml:"time_maps_page"`
	// DefaultTimeMap is the graph-level fallback Time Map slug.
	// Explicit time-map:: on block, parent or page wins over it; it wins
	// over the global default:: true map.
	DefaultTimeMap string `yaml:"default_time_map"`
}

// Keep is the Google Keep backend source (keep-integration.md,
// ADR 0026): note sets read through the official Workspace API or the
// unofficial personal-account sync transport, merged like Logseq graphs.
type Keep struct {
	// SyncURL is the unofficial sync transport root (personal accounts).
	// Empty means unconfigured: unofficial sources fail closed with an
	// actionable error instead of dialing a guessed host.
	SyncURL string `yaml:"sync_url"`
	// Sources lists the Keep note sets to merge, in priority order after
	// the Logseq graphs. Empty means no Keep backend.
	Sources []KeepSource `yaml:"sources"`
}

// KeepSource is one Keep note set in a mixed-source setup.
type KeepSource struct {
	// Name is the source slug (personal, family). It fills Task.Graph,
	// check/forecast output and the calendar graph field, like a graph.
	Name string `yaml:"name"`
	// Backend is keep-official (Workspace, OAuth) or keep-unofficial
	// (personal, sync token). Required per source.
	Backend string `yaml:"backend"`
	// Label selects the notes: only notes carrying this Keep label are
	// read. Empty means every untrashed, unarchived note.
	Label string `yaml:"label"`
	// DefaultTimeMap is the source-level fallback Time Map slug.
	// Explicit time-map:: on a block wins over it; it wins over the
	// global default:: true map.
	DefaultTimeMap string `yaml:"default_time_map"`
}

// Scheduling settings.
type Scheduling struct {
	// Granularity is the calendar's quantum, a whole number of minutes that
	// divides 60 (15m, 20m, 30m), and the floor for small Tasks.
	Granularity string `yaml:"granularity"`
	// WindowDays is the rolling Scheduling window; nothing beyond it is
	// scheduled or synced.
	WindowDays int `yaml:"window_days"`
	// Timezone names the zone Time Map hours are interpreted in: Time Map
	// hours are absolute local times, so this is what "09:00" means.
	Timezone string `yaml:"timezone"`
}

// Calendar is the single output Calendar.
type Calendar struct {
	// Provider is "google" in v0.1.
	Provider string `yaml:"provider"`
	// ID selects the one Google Calendar; default "primary".
	ID string `yaml:"id"`
	// ReadCalendars holds shared calendar IDs read for busy time only
	// (person shares use the owner email, resources and groups use their
	// IDs). Empty means the write calendar alone. Never written to.
	ReadCalendars []string `yaml:"read_calendars"`
	// IgnoreReadAllDay drops every all-day event on a read calendar from
	// busy time. The write calendar is unchanged. False keeps all-day events
	// busy, which is the default.
	IgnoreReadAllDay bool `yaml:"ignore_read_all_day"`
	// IgnoreReadAllDayMatching drops an all-day read-calendar event whose
	// summary contains one of these phrases, case-insensitively. Timed
	// events are never matched. Unused when IgnoreReadAllDay is true.
	IgnoreReadAllDayMatching []string `yaml:"ignore_read_all_day_matching"`
}

// ParseGranularity parses a configured scheduling.granularity: a positive
// duration that is a whole number of minutes dividing 60, so every Block is
// whole granularity units and every hour is a whole number of Blocks. 25m is
// therefore a configuration error, not a quirky schedule.
func ParseGranularity(s string) (time.Duration, error) {
	d, err := parse.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("scheduling.granularity %q: %w", s, err)
	}
	if d%time.Minute != 0 {
		return 0, fmt.Errorf("scheduling.granularity %q: must be a whole number of minutes", s)
	}
	if minutes := int(d / time.Minute); 60%minutes != 0 {
		return 0, fmt.Errorf("scheduling.granularity %q: must divide 60 minutes", s)
	}
	return d, nil
}

// Location returns the configured Scheduling timezone.
func (s Scheduling) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("scheduling.timezone %q: %w", s.Timezone, err)
	}
	return loc, nil
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config file %s: %w (pass --config to override; default %s)", path, err, DefaultConfig)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config file %s: %w", path, err)
	}
	return validate(cfg)
}

// DefaultNames defaults supplied by validate; documented in the error when a
// required setting is missing.
const (
	// DefaultTimeMapsPage is the page Time Maps are authored on (ADR 0003).
	DefaultTimeMapsPage = "Time Maps"
	// DefaultCalendarID is the single output Calendar.
	DefaultCalendarID = "primary"
	// DefaultConfig documents the usual config location in error messages.
	DefaultConfig = "${XDG_CONFIG_HOME:-~/.config}/lodo/config.yaml"
	// DefaultCredentials documents the usual credentials location.
	DefaultCredentials = "${XDG_STATE_HOME:-~/.local/state}/lodo/credentials.json"
)

// validate normalises cfg (trimming every string setting) and checks it,
// returning the effective config with defaults applied.
func validate(cfg Config) (Config, error) {
	cfg.Logseq.APIURL = strings.TrimSpace(cfg.Logseq.APIURL)
	cfg.Logseq.TimeMapsPage = strings.TrimSpace(cfg.Logseq.TimeMapsPage)
	cfg.Keep.SyncURL = strings.TrimSpace(cfg.Keep.SyncURL)
	for i := range cfg.Keep.Sources {
		s := &cfg.Keep.Sources[i]
		s.Name = strings.TrimSpace(s.Name)
		s.Backend = strings.TrimSpace(s.Backend)
		s.Label = strings.TrimSpace(s.Label)
		s.DefaultTimeMap = strings.TrimSpace(s.DefaultTimeMap)
	}
	for i := range cfg.Logseq.Graphs {
		g := &cfg.Logseq.Graphs[i]
		g.Name = strings.TrimSpace(g.Name)
		g.APIURL = strings.TrimSpace(g.APIURL)
		g.TimeMapsPage = strings.TrimSpace(g.TimeMapsPage)
		g.DefaultTimeMap = strings.TrimSpace(g.DefaultTimeMap)
	}
	cfg.Scheduling.Granularity = strings.TrimSpace(cfg.Scheduling.Granularity)
	cfg.Scheduling.Timezone = strings.TrimSpace(cfg.Scheduling.Timezone)
	cfg.Calendar.Provider = strings.TrimSpace(cfg.Calendar.Provider)
	cfg.Calendar.ID = strings.TrimSpace(cfg.Calendar.ID)
	cfg.UI.Theme = strings.TrimSpace(cfg.UI.Theme)

	if len(cfg.Logseq.Graphs) == 0 && cfg.Logseq.APIURL == "" && len(cfg.Keep.Sources) == 0 {
		return Config{}, fmt.Errorf("logseq.api_url: required")
	}
	if cfg.Logseq.APIURL != "" {
		if err := checkAPIURL(cfg.Logseq.APIURL); err != nil {
			return Config{}, err
		}
	}
	if len(cfg.Logseq.Graphs) > 0 {
		if err := validateGraphs(cfg.Logseq.Graphs); err != nil {
			return Config{}, err
		}
	}
	if len(cfg.Keep.Sources) > 0 {
		if err := validateKeepSources(cfg.Logseq.Graphs, cfg.Keep.Sources); err != nil {
			return Config{}, err
		}
		if cfg.Keep.SyncURL != "" {
			if err := checkKeepSyncURL(cfg.Keep.SyncURL); err != nil {
				return Config{}, err
			}
		}
	}
	if cfg.Logseq.TimeMapsPage == "" {
		cfg.Logseq.TimeMapsPage = DefaultTimeMapsPage
	}
	for i := range cfg.Logseq.Graphs {
		if strings.TrimSpace(cfg.Logseq.Graphs[i].TimeMapsPage) == "" {
			cfg.Logseq.Graphs[i].TimeMapsPage = cfg.Logseq.TimeMapsPage
		}
	}
	if cfg.Scheduling.Granularity == "" {
		return Config{}, fmt.Errorf("scheduling.granularity: required (e.g. 15m: a whole number of minutes that divides 60)")
	}
	if _, err := ParseGranularity(cfg.Scheduling.Granularity); err != nil {
		return Config{}, err
	}
	if cfg.Scheduling.WindowDays <= 0 {
		return Config{}, fmt.Errorf("scheduling.window_days: must be positive, got %d", cfg.Scheduling.WindowDays)
	}
	if cfg.Scheduling.Timezone == "" {
		return Config{}, fmt.Errorf("scheduling.timezone: required (e.g. Europe/London)")
	}
	// The timezone must load cleanly on a bare machine (ADR 0009: the tz
	// database ships inside the binary).
	if _, err := cfg.Scheduling.Location(); err != nil {
		return Config{}, err
	}
	if cfg.Calendar.Provider == "" {
		return Config{}, fmt.Errorf(`calendar.provider: required (want "google" in v0.1)`)
	}
	if cfg.Calendar.Provider != "google" {
		return Config{}, fmt.Errorf("calendar.provider %q: want \"google\" in v0.1", cfg.Calendar.Provider)
	}
	if cfg.Calendar.ID == "" {
		cfg.Calendar.ID = DefaultCalendarID
	}
	cfg.Calendar.ReadCalendars = norm.ReadCalendars(cfg.Calendar.ID, cfg.Calendar.ReadCalendars)
	cfg.Calendar.IgnoreReadAllDayMatching = norm.Phrases(cfg.Calendar.IgnoreReadAllDayMatching)
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = theme.Concrete
	}
	return cfg, nil
}

// checkAPIURL validates one absolute http(s) URL.
func checkAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("logseq.api_url %q: must be an absolute http(s) URL", raw)
	}
	return nil
}

// validateGraphs checks names (required, slugifiable, unique
// case-insensitively), per-graph URLs and default_time_map slugs.
func validateGraphs(graphs []GraphSource) error {
	seen := set.New[string]()
	for i, g := range graphs {
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("logseq.graphs[%d].name: required (e.g. work)", i)
		}
		slug := parse.Slugify(g.Name)
		if slug == "" {
			return fmt.Errorf("logseq.graphs[%d].name %q: must slugify to a non-empty key", i, g.Name)
		}
		if !seen.Add(slug) {
			return fmt.Errorf("logseq.graphs[%d].name %q: duplicate graph name (slug %s)", i, g.Name, slug)
		}
		if g.APIURL == "" {
			return fmt.Errorf("logseq.graphs[%d].api_url: required", i)
		}
		u, err := url.Parse(g.APIURL)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("logseq.graphs[%d].api_url %q: must be an absolute http(s) URL", i, g.APIURL)
		}
		if g.DefaultTimeMap != "" && parse.Slugify(g.DefaultTimeMap) == "" {
			return fmt.Errorf("logseq.graphs[%d].default_time_map %q: must slugify to a non-empty key", i, g.DefaultTimeMap)
		}
	}
	return nil
}

// validateKeepSources checks Keep source names (required, slugifiable,
// unique — including against Logseq graph names, which share the Task.Graph
// namespace), backends (keep-official or keep-unofficial) and default
// time-map slugs.
func validateKeepSources(graphs []GraphSource, sources []KeepSource) error {
	taken := set.New[string]()
	for _, g := range graphs {
		if slug := parse.Slugify(g.Name); slug != "" {
			taken.Add(slug)
		}
	}
	for i, s := range sources {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("keep.sources[%d].name: required (e.g. personal)", i)
		}
		slug := parse.Slugify(s.Name)
		if slug == "" {
			return fmt.Errorf("keep.sources[%d].name %q: must slugify to a non-empty key", i, s.Name)
		}
		if !taken.Add(slug) {
			return fmt.Errorf("keep.sources[%d].name %q: duplicate source name (slug %s)", i, s.Name, slug)
		}
		if s.Backend == "" {
			return fmt.Errorf("keep.sources[%d].backend: required (want %q or %q)", i, keep.BackendWorkspace, keep.BackendPersonal)
		}
		if !keep.ValidBackend(s.Backend) {
			return fmt.Errorf("keep.sources[%d].backend %q: want %q or %q", i, s.Backend, keep.BackendWorkspace, keep.BackendPersonal)
		}
		if s.DefaultTimeMap != "" && parse.Slugify(s.DefaultTimeMap) == "" {
			return fmt.Errorf("keep.sources[%d].default_time_map %q: must slugify to a non-empty key", i, s.DefaultTimeMap)
		}
	}
	return nil
}

// checkKeepSyncURL validates one absolute http(s) sync URL.
func checkKeepSyncURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("keep.sync_url %q: must be an absolute http(s) URL", raw)
	}
	return nil
}

// the first graph time_maps_page override, else the global one.
// PrimaryTimeMapsPage is the canonical page Time Maps are read from:
func (l Logseq) PrimaryTimeMapsPage() string {
	if len(l.Graphs) > 0 && strings.TrimSpace(l.Graphs[0].TimeMapsPage) != "" {
		return strings.TrimSpace(l.Graphs[0].TimeMapsPage)
	}
	if strings.TrimSpace(l.TimeMapsPage) != "" {
		return strings.TrimSpace(l.TimeMapsPage)
	}
	return DefaultTimeMapsPage
}

// GraphDefault returns the source-level fallback Time Map slug for name,
// slugified, or empty when none is configured. Lookup is case-insensitive
// across Logseq graphs and Keep sources.
func (l Logseq) GraphDefault(name string) string {
	want := parse.Slugify(name)
	for _, g := range l.Graphs {
		if parse.Slugify(g.Name) == want {
			return parse.Slugify(g.DefaultTimeMap)
		}
	}
	return ""
}

// SourceDefault returns the fallback Time Map slug for any source name,
// Logseq graph or Keep source.
func (c Config) SourceDefault(name string) string {
	if slug := c.Logseq.GraphDefault(name); slug != "" {
		return slug
	}
	want := parse.Slugify(name)
	for _, s := range c.Keep.Sources {
		if parse.Slugify(s.Name) == want {
			return parse.Slugify(s.DefaultTimeMap)
		}
	}
	return ""
}

// GraphOrder returns the config index of name, or -1 when unknown.
// Single-graph mode returns 0 for any name.
func (l Logseq) GraphOrder(name string) int {
	if len(l.Graphs) == 0 {
		return 0
	}
	want := parse.Slugify(name)
	for i, g := range l.Graphs {
		if parse.Slugify(g.Name) == want {
			return i
		}
	}
	return -1
}

// SourceOrder returns the deterministic cross-source index of name:
// Logseq graphs first, Keep sources after, or -1 when unknown.
// Single-source mode returns 0 for any name.
func (c Config) SourceOrder(name string) int {
	if len(c.Logseq.Graphs) == 0 && len(c.Keep.Sources) == 0 {
		return 0
	}
	want := parse.Slugify(name)
	for i, g := range c.Logseq.Graphs {
		if parse.Slugify(g.Name) == want {
			return i
		}
	}
	for i, s := range c.Keep.Sources {
		if parse.Slugify(s.Name) == want {
			return len(c.Logseq.Graphs) + i
		}
	}
	return -1
}

// normalizeReadCalendars trims entries, drops empties, silently drops any
// read entry equal to the write calendar ID, and drops exact duplicates
// within the read list, preserving config order. Nil in stays nil out when
// nothing survives, so an absent list and an emptied one compare equal.
func normalizeReadCalendars(writeID string, reads []string) []string {
	return norm.ReadCalendars(writeID, reads)
}

// Overrides are one-run CLI overrides (configuration.md): any setting may be
// overridden for a single run by a flag; overrides are never persisted.
type Overrides struct {
	// WindowDays overrides scheduling.window_days; 0 keeps the config value.
	WindowDays int
	// Timezone overrides scheduling.timezone; "" keeps the config value.
	Timezone string
	// ReadCalendars replaces calendar.read_calendars for one run when
	// non-nil (the --read-calendar flag given at least once). Nil keeps the
	// config value; a non-nil but empty-after-normalization list means the
	// write calendar alone.
	ReadCalendars []string
}

// Apply resolves config plus Overrides into the effective config. It is a
// pure function: neither argument is mutated and nothing is written down.
func Apply(cfg Config, o Overrides) (Config, error) {
	if o.WindowDays < 0 {
		return Config{}, fmt.Errorf("--window-days %d: must be positive", o.WindowDays)
	}
	if o.WindowDays > 0 {
		cfg.Scheduling.WindowDays = o.WindowDays
	}
	if o.Timezone != "" {
		if _, err := time.LoadLocation(o.Timezone); err != nil {
			return Config{}, fmt.Errorf("--timezone %q: %w", o.Timezone, err)
		}
		cfg.Scheduling.Timezone = o.Timezone
	}
	if o.ReadCalendars != nil {
		// Copy before normalizing: the caller's slice is never mutated.
		cp := append([]string(nil), o.ReadCalendars...)
		cfg.Calendar.ReadCalendars = normalizeReadCalendars(cfg.Calendar.ID, cp)
	}
	return cfg, nil
}

// Credentials is the contents of credentials.json: the logseq section that
// Lodo owns for the loop commands (M3), and the google section that the
// OAuth flow fills in (calendar.md Auth). Unknown sections are tolerated so
// adding one does not invalidate the file.
type Credentials struct {
	Logseq LogseqCredentials  `json:"logseq"`
	Keep   KeepCredentials    `json:"keep,omitempty"`
	Google *GoogleCredentials `json:"google,omitempty"`

	// FlagGraphTokens holds one-run CLI flag overrides for per-graph tokens,
	// keyed by slug. Flags win over environment and file (configuration.md
	// Precedence). Runtime only, never persisted.
	FlagGraphTokens map[string]string `json:"-"`
	// FlagKeepTokens holds one-run CLI flag overrides for per-source Keep
	// tokens, keyed by slug. Runtime only, never persisted.
	FlagKeepTokens map[string]string `json:"-"`
}

// LogseqCredentials carries the Logseq HTTP API token.
type LogseqCredentials struct {
	Token string `json:"token"`
	// Graphs holds per-graph tokens, keyed by graph name (work, family).
	Graphs map[string]LogseqGraphCredentials `json:"graphs,omitempty"`
}

// LogseqGraphCredentials is one graph token.
type LogseqGraphCredentials struct {
	Token string `json:"token"`
}

// KeepCredentials carries the unofficial Keep sync token (personal
// accounts). The official backend reuses the google section instead.
type KeepCredentials struct {
	Token string `json:"token,omitempty"`
	// Sources holds per-source tokens, keyed by source name.
	Sources map[string]KeepSourceCredentials `json:"sources,omitempty"`
}

// KeepSourceCredentials is one Keep source token.
type KeepSourceCredentials struct {
	Token string `json:"token"`
}

// GoogleCredentials is the google section: the OAuth client the user
// registered and pasted into the file before `lodo auth google` runs, plus
// the refresh token that flow stores once and the Calendar client renews
// with (configuration.md).
type GoogleCredentials struct {
	// RefreshToken is the one long-lived Google grant; empty until
	// `lodo auth google` has stored it.
	RefreshToken string `json:"refresh_token,omitempty"`
	// ClientID is the OAuth client's id.
	ClientID string `json:"client_id,omitempty"`
	// ClientSecret is the OAuth client's secret.
	ClientSecret string `json:"client_secret,omitempty"`
}

// Environment variables that override credentials.json, one field each.
// Non-empty values win over the file; empty values leave the file untouched.
// A missing file plus a complete-enough environment is a valid setup, which
// is what containers and CI use.
const (
	// EnvLogseqToken carries the Logseq HTTP API token.
	EnvLogseqToken = "LODO_LOGSEQ_TOKEN"
	// EnvGoogleClientID carries the Google OAuth client id.
	EnvGoogleClientID = "LODO_GOOGLE_CLIENT_ID"
	// EnvGoogleClientSecret carries the Google OAuth client secret.
	EnvGoogleClientSecret = "LODO_GOOGLE_CLIENT_SECRET"
	// EnvGoogleRefreshToken carries the Google OAuth refresh token that
	// `lodo auth google` would otherwise store in the file.
	EnvGoogleRefreshToken = "LODO_GOOGLE_REFRESH_TOKEN"
	// EnvKeepToken carries the unofficial Keep sync token (personal
	// accounts). The official backend reuses the Google grant instead.
	EnvKeepToken = "LODO_KEEP_TOKEN"
	// EnvKeepSyncURL carries the unofficial Keep sync endpoint root.
	EnvKeepSyncURL = "LODO_KEEP_SYNC_URL"
)

// GraphTokenEnvVar names the per-graph token variable:
// work -> LODO_LOGSEQ_TOKEN_WORK, my-work -> LODO_LOGSEQ_TOKEN_MY_WORK.
func GraphTokenEnvVar(graph string) string {
	slug := parse.Slugify(graph)
	slug = strings.ReplaceAll(slug, "-", "_")
	return EnvLogseqToken + "_" + strings.ToUpper(slug)
}

// TokenForGraph resolves the effective token for one graph: one-run CLI
// flags win, then per-graph env, then the per-graph file entry
// (case-insensitive), then the global token (file+env fallback already
// applied). Empty means unconfigured.
func (c Credentials) TokenForGraph(graph string) string {
	want := parse.Slugify(graph)
	if want != "" && c.FlagGraphTokens != nil {
		if v, ok := c.FlagGraphTokens[want]; ok {
			if t := strings.TrimSpace(v); t != "" {
				return t
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv(GraphTokenEnvVar(graph))); v != "" {
		return v
	}
	for name, g := range c.Logseq.Graphs {
		if parse.Slugify(name) == want {
			if v := strings.TrimSpace(g.Token); v != "" {
				return v
			}
			break
		}
	}
	return strings.TrimSpace(c.Logseq.Token)
}

// KeepTokenEnvVar names the per-source Keep token variable:
// personal -> LODO_KEEP_TOKEN_PERSONAL.
func KeepTokenEnvVar(source string) string {
	slug := parse.Slugify(source)
	slug = strings.ReplaceAll(slug, "-", "_")
	return EnvKeepToken + "_" + strings.ToUpper(slug)
}

// TokenForKeepSource resolves the effective unofficial sync token for one
// Keep source: one-run CLI flags win, then per-source env, then the
// per-source file entry (case-insensitive), then the global token.
// Empty means unconfigured (or the official backend, which needs no
// token).
func (c Credentials) TokenForKeepSource(source string) string {
	want := parse.Slugify(source)
	if want != "" && c.FlagKeepTokens != nil {
		if v, ok := c.FlagKeepTokens[want]; ok {
			if t := strings.TrimSpace(v); t != "" {
				return t
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv(KeepTokenEnvVar(source))); v != "" {
		return v
	}
	for name, g := range c.Keep.Sources {
		if parse.Slugify(name) == want {
			if v := strings.TrimSpace(g.Token); v != "" {
				return v
			}
			break
		}
	}
	return strings.TrimSpace(c.Keep.Token)
}

// HasAnyLogseqToken reports whether any Logseq token exists: global file,
// per-graph file, global env or any LODO_LOGSEQ_TOKEN_* env var.
func (c Credentials) HasAnyLogseqToken() bool {
	if strings.TrimSpace(c.Logseq.Token) != "" {
		return true
	}
	for _, g := range c.Logseq.Graphs {
		if strings.TrimSpace(g.Token) != "" {
			return true
		}
	}
	for _, v := range c.FlagGraphTokens {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if k == EnvLogseqToken {
			return true
		}
		if _, ok := strings.CutPrefix(k, EnvLogseqToken+"_"); ok {
			return true
		}
	}
	return false
}

// HasAnyKeepToken reports whether any unofficial Keep sync token exists:
// global file, per-source file, global env or any LODO_KEEP_TOKEN_* env
// var. The official backend needs no token (it reuses the Google grant).
func (c Credentials) HasAnyKeepToken() bool {
	if strings.TrimSpace(c.Keep.Token) != "" {
		return true
	}
	for _, g := range c.Keep.Sources {
		if strings.TrimSpace(g.Token) != "" {
			return true
		}
	}
	for _, v := range c.FlagKeepTokens {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if k == EnvKeepToken {
			return true
		}
		if _, ok := strings.CutPrefix(k, EnvKeepToken+"_"); ok {
			return true
		}
	}
	return false
}

// trimmed variable becomes its field. A Google section is present when any
// of its three variables is non-empty.
// CredentialsFromEnv reads the credential environment: each non-empty,
// trimmed variable becomes its field. A Google section is present when any
// of its three variables is non-empty.
func CredentialsFromEnv() Credentials {
	var c Credentials
	c.Logseq.Token = strings.TrimSpace(os.Getenv(EnvLogseqToken))
	for _, e := range os.Environ() {
		const prefix = EnvLogseqToken + "_"
		key, value, ok := strings.Cut(e, "=")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		suffix, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		suffix = strings.ToLower(strings.TrimSpace(suffix))
		suffix = strings.ReplaceAll(suffix, "_", "-")
		if parse.Slugify(suffix) == "" {
			continue
		}
		if c.Logseq.Graphs == nil {
			c.Logseq.Graphs = map[string]LogseqGraphCredentials{}
		}
		// Key by slug; TokenForGraph matches case-insensitively anyway.
		c.Logseq.Graphs[suffix] = LogseqGraphCredentials{Token: strings.TrimSpace(value)}
	}
	c.Keep.Token = strings.TrimSpace(os.Getenv(EnvKeepToken))
	for _, e := range os.Environ() {
		const keepPrefix = EnvKeepToken + "_"
		key, value, ok := strings.Cut(e, "=")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		suffix, ok := strings.CutPrefix(key, keepPrefix)
		if !ok {
			continue
		}
		suffix = strings.ToLower(strings.TrimSpace(suffix))
		suffix = strings.ReplaceAll(suffix, "_", "-")
		if parse.Slugify(suffix) == "" {
			continue
		}
		if c.Keep.Sources == nil {
			c.Keep.Sources = map[string]KeepSourceCredentials{}
		}
		// Key by slug; TokenForKeepSource matches case-insensitively anyway.
		c.Keep.Sources[suffix] = KeepSourceCredentials{Token: strings.TrimSpace(value)}
	}
	if id := strings.TrimSpace(os.Getenv(EnvGoogleClientID)); id != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		}
		c.Google.ClientID = id
	}
	if secret := strings.TrimSpace(os.Getenv(EnvGoogleClientSecret)); secret != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		}
		c.Google.ClientSecret = secret
	}
	if refresh := strings.TrimSpace(os.Getenv(EnvGoogleRefreshToken)); refresh != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		}
		c.Google.RefreshToken = refresh
	}
	return c
}

// isEmpty reports whether neither the file nor the environment supplied a
// secret: no Logseq token, no Keep token and no Google field at all.
func (c Credentials) isEmpty() bool {
	if c.Logseq.Token != "" {
		return false
	}
	for _, g := range c.Logseq.Graphs {
		if strings.TrimSpace(g.Token) != "" {
			return false
		}
	}
	if strings.TrimSpace(c.Keep.Token) != "" {
		return false
	}
	for _, g := range c.Keep.Sources {
		if strings.TrimSpace(g.Token) != "" {
			return false
		}
	}
	if c.Google == nil {
		return true
	}
	return c.Google.ClientID == "" && c.Google.ClientSecret == "" && c.Google.RefreshToken == ""
}

// canonicalGraphTokens rekeys per-graph tokens by slug so "Work" and "work"
// collapse to one entry. Later keys win on collision; callers pass maps in
// precedence order (file, then env, then flags) with sorted keys for
// determinism.
func canonicalGraphTokens(in map[string]LogseqGraphCredentials) map[string]LogseqGraphCredentials {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]LogseqGraphCredentials, len(in))
	keys := slices.Sorted(maps.Keys(in))
	for _, k := range keys {
		slug := parse.Slugify(k)
		if slug == "" {
			continue
		}
		out[slug] = in[k]
	}
	return out
}

// canonicalKeepTokens rekeys per-source Keep tokens by slug, like
// canonicalGraphTokens.
func canonicalKeepTokens(in map[string]KeepSourceCredentials) map[string]KeepSourceCredentials {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]KeepSourceCredentials, len(in))
	keys := slices.Sorted(maps.Keys(in))
	for _, k := range keys {
		slug := parse.Slugify(k)
		if slug == "" {
			continue
		}
		out[slug] = in[k]
	}
	return out
}

// withEnv overlays the environment onto c: non-empty variables win per
// field, empty ones keep the file's value. The receiver is never mutated.
func (c Credentials) withEnv() Credentials {
	env := CredentialsFromEnv()
	if env.Logseq.Token != "" {
		c.Logseq.Token = env.Logseq.Token
	}
	if len(env.Logseq.Graphs) > 0 {
		merged := make(map[string]LogseqGraphCredentials, len(c.Logseq.Graphs)+len(env.Logseq.Graphs))
		for k, v := range canonicalGraphTokens(c.Logseq.Graphs) {
			merged[k] = v
		}
		for k, v := range env.Logseq.Graphs {
			merged[k] = v
		}
		c.Logseq.Graphs = merged
	} else if len(c.Logseq.Graphs) > 0 {
		c.Logseq.Graphs = canonicalGraphTokens(c.Logseq.Graphs)
	}
	if c.FlagGraphTokens != nil {
		cp := make(map[string]string, len(c.FlagGraphTokens))
		maps.Copy(cp, c.FlagGraphTokens)
		c.FlagGraphTokens = cp
	}
	if env.Keep.Token != "" {
		c.Keep.Token = env.Keep.Token
	}
	if len(env.Keep.Sources) > 0 {
		merged := make(map[string]KeepSourceCredentials, len(c.Keep.Sources)+len(env.Keep.Sources))
		for k, v := range canonicalKeepTokens(c.Keep.Sources) {
			merged[k] = v
		}
		for k, v := range env.Keep.Sources {
			merged[k] = v
		}
		c.Keep.Sources = merged
	} else if len(c.Keep.Sources) > 0 {
		c.Keep.Sources = canonicalKeepTokens(c.Keep.Sources)
	}
	if c.FlagKeepTokens != nil {
		cp := make(map[string]string, len(c.FlagKeepTokens))
		maps.Copy(cp, c.FlagKeepTokens)
		c.FlagKeepTokens = cp
	}
	if env.Google != nil {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		} else {
			cp := *c.Google
			c.Google = &cp
		}
		if env.Google.ClientID != "" {
			c.Google.ClientID = env.Google.ClientID
		}
		if env.Google.ClientSecret != "" {
			c.Google.ClientSecret = env.Google.ClientSecret
		}
		if env.Google.RefreshToken != "" {
			c.Google.RefreshToken = env.Google.RefreshToken
		}
	}
	return c
}

// CredentialOverrides are one-run CLI overrides for secrets
// (configuration.md Precedence): non-empty flags win per field over the
// environment and the file; overrides are never persisted.
type CredentialOverrides struct {
	// LogseqToken overrides logseq.token; "" keeps the file/environment value.
	LogseqToken string
	// LogseqGraphTokens overrides per-graph tokens by graph name (slug,
	// case-insensitive); empty values are ignored. Never persisted.
	LogseqGraphTokens map[string]string
	// KeepToken overrides keep.token (unofficial sync token); "" keeps the
	// file/environment value.
	KeepToken string
	// KeepSourceTokens overrides per-source Keep tokens by source name
	// (slug, case-insensitive); empty values are ignored. Never persisted.
	KeepSourceTokens map[string]string
	// GoogleClientID overrides google.client_id; "" keeps the file/environment value.
	GoogleClientID string
	// GoogleClientSecret overrides google.client_secret; "" keeps the file/environment value.
	GoogleClientSecret string
	// GoogleRefreshToken overrides google.refresh_token; "" keeps the file/environment value.
	GoogleRefreshToken string
}

// WithCredentialOverrides resolves credentials plus CredentialOverrides into
// the effective credentials. It is a pure function: neither argument is
// mutated and nothing is written down.
func (c Credentials) WithCredentialOverrides(o CredentialOverrides) Credentials {
	if v := strings.TrimSpace(o.LogseqToken); v != "" {
		c.Logseq.Token = v
	}
	if len(o.LogseqGraphTokens) > 0 {
		flags := make(map[string]string, len(o.LogseqGraphTokens))
		if c.FlagGraphTokens != nil {
			maps.Copy(flags, c.FlagGraphTokens)
		}
		merged := canonicalGraphTokens(c.Logseq.Graphs)
		if merged == nil {
			merged = map[string]LogseqGraphCredentials{}
		}
		keys := slices.Sorted(maps.Keys(o.LogseqGraphTokens))
		for _, k := range keys {
			if vv := strings.TrimSpace(o.LogseqGraphTokens[k]); vv != "" {
				if slug := parse.Slugify(k); slug != "" {
					flags[slug] = vv
					merged[slug] = LogseqGraphCredentials{Token: vv}
				}
			}
		}
		c.FlagGraphTokens = flags
		c.Logseq.Graphs = merged
	} else if c.FlagGraphTokens != nil {
		cp := make(map[string]string, len(c.FlagGraphTokens))
		maps.Copy(cp, c.FlagGraphTokens)
		c.FlagGraphTokens = cp
		if len(c.Logseq.Graphs) > 0 {
			c.Logseq.Graphs = canonicalGraphTokens(c.Logseq.Graphs)
		}
	} else if len(c.Logseq.Graphs) > 0 {
		c.Logseq.Graphs = canonicalGraphTokens(c.Logseq.Graphs)
	}
	if v := strings.TrimSpace(o.KeepToken); v != "" {
		c.Keep.Token = v
	}
	if len(o.KeepSourceTokens) > 0 {
		flags := make(map[string]string, len(o.KeepSourceTokens))
		if c.FlagKeepTokens != nil {
			maps.Copy(flags, c.FlagKeepTokens)
		}
		merged := canonicalKeepTokens(c.Keep.Sources)
		if merged == nil {
			merged = map[string]KeepSourceCredentials{}
		}
		keys := slices.Sorted(maps.Keys(o.KeepSourceTokens))
		for _, k := range keys {
			if vv := strings.TrimSpace(o.KeepSourceTokens[k]); vv != "" {
				if slug := parse.Slugify(k); slug != "" {
					flags[slug] = vv
					merged[slug] = KeepSourceCredentials{Token: vv}
				}
			}
		}
		c.FlagKeepTokens = flags
		c.Keep.Sources = merged
	} else if c.FlagKeepTokens != nil {
		cp := make(map[string]string, len(c.FlagKeepTokens))
		maps.Copy(cp, c.FlagKeepTokens)
		c.FlagKeepTokens = cp
		if len(c.Keep.Sources) > 0 {
			c.Keep.Sources = canonicalKeepTokens(c.Keep.Sources)
		}
	} else if len(c.Keep.Sources) > 0 {
		c.Keep.Sources = canonicalKeepTokens(c.Keep.Sources)
	}
	if v := strings.TrimSpace(o.GoogleClientID); v != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		} else {
			cp := *c.Google
			c.Google = &cp
		}
		c.Google.ClientID = v
	}
	if v := strings.TrimSpace(o.GoogleClientSecret); v != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		} else {
			cp := *c.Google
			c.Google = &cp
		}
		c.Google.ClientSecret = v
	}
	if v := strings.TrimSpace(o.GoogleRefreshToken); v != "" {
		if c.Google == nil {
			c.Google = &GoogleCredentials{}
		} else {
			cp := *c.Google
			c.Google = &cp
		}
		c.Google.RefreshToken = v
	}
	return c
}

// GoogleSetup returns the google section as `lodo auth google` needs it:
// the client pair, still without demanding a refresh token. Missing
// entries are refusals with the fix, never prompts.
func (c Credentials) GoogleSetup() (GoogleCredentials, error) {
	var zero GoogleCredentials
	if c.Google == nil {
		return zero, fmt.Errorf(
			"google credentials: missing (create an OAuth client with redirect URI http://127.0.0.1/ and paste {\"google\":{\"client_id\":\"…\",\"client_secret\":\"…\"}} into %s, or set %s and %s)", DefaultCredentials, EnvGoogleClientID, EnvGoogleClientSecret)
	}
	g := *c.Google
	if g.ClientID == "" || g.ClientSecret == "" {
		return zero, fmt.Errorf(
			"google credentials: client_id and client_secret are required; paste them into %s from the OAuth client with redirect URI http://127.0.0.1/, or set %s and %s", DefaultCredentials, EnvGoogleClientID, EnvGoogleClientSecret)
	}
	return g, nil
}

// GoogleAuth returns the google section as the Calendar client needs it:
// the client pair plus the refresh token. An empty refresh token means the
// auth flow has not run yet; the fix is to run it once.
func (c Credentials) GoogleAuth() (GoogleCredentials, error) {
	g, err := c.GoogleSetup()
	if err != nil {
		return GoogleCredentials{}, err
	}
	if g.RefreshToken == "" {
		return GoogleCredentials{}, fmt.Errorf(
			"google credentials: refresh_token is empty; run `lodo auth google` once to store it in %s, or set %s", DefaultCredentials, EnvGoogleRefreshToken)
	}
	return g, nil
}

// ReadFileCredentials reads credentials.json at path without consulting
// the environment: the 0600 file exactly as written. Persist uses it so an
// environment-only Logseq token is never copied into the file as a side
// effect of storing the Google refresh token.
func ReadFileCredentials(path string) (Credentials, error) {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return Credentials{}, fmt.Errorf("credentials file %s: not found (default %s)", path, DefaultCredentials)
	case err != nil:
		return Credentials{}, fmt.Errorf("credentials file %s: %w", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		return Credentials{}, fmt.Errorf("credentials file %s: want mode 0600, got %04o; refusing a file others can read", path, got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("credentials file %s: %w", path, err)
	}
	var c Credentials
	if err := jsonx.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("credentials file %s: %w", path, err)
	}
	return c, nil
}

// ReadCredentials reads credentials.json at path and overlays the
// credential environment (LODO_LOGSEQ_TOKEN, LODO_KEEP_TOKEN,
// LODO_GOOGLE_CLIENT_ID, LODO_GOOGLE_CLIENT_SECRET,
// LODO_GOOGLE_REFRESH_TOKEN, plus per-graph/source suffixed tokens):
// non-empty
// variables win per field. The file must be 0600 when present: a
// credentials file readable by group or others is a refusal, not a warning.
// A missing file plus a non-empty environment is valid, so containers can
// run without a file at all. Nothing else is demanded here: which section
// is required is the caller's call — the loop commands need a task-backend
// credential, `auth google` needs the Google client pair, and both live in
// the same file or environment.
func ReadCredentials(path string) (Credentials, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if env := CredentialsFromEnv(); !env.isEmpty() {
			return env, nil
		}
		return Credentials{}, fmt.Errorf("credentials file %s: not found (default %s)", path, DefaultCredentials)
	}
	c, err := ReadFileCredentials(path)
	if err != nil {
		return Credentials{}, err
	}
	return c.withEnv(), nil
}

// LoadCredentials reads credentials.json at path for the loop commands: the
// 0600 file (or environment) plus a usable task-backend credential — a
// Logseq token, an unofficial Keep sync token, or a complete Google grant
// (which serves the official Keep backend and the Calendar). Per-source
// requirements are enforced later by buildSources, which names the exact
// source and variable. The google section travels along as it is written;
// the Calendar side validates its own completeness.
func LoadCredentials(path string) (Credentials, error) {
	c, err := ReadCredentials(path)
	if err != nil {
		return Credentials{}, err
	}
	if !c.HasAnyLogseqToken() && !c.HasAnyKeepToken() {
		if _, gerr := c.GoogleAuth(); gerr != nil {
			return Credentials{}, fmt.Errorf("credentials file %s: logseq.token is empty; paste the Logseq API token from the app (Settings → Integrations), set %s, or add per-graph tokens (logseq.graphs.*.token / LODO_LOGSEQ_TOKEN_<GRAPH>); for Keep, set %s (unofficial) or complete the Google grant (official, keep scope)", path, EnvLogseqToken, EnvKeepToken)
		}
	}
	return c, nil
}

// SaveCredentials writes c to path, creating the state directory if needed.
// The file is written 0600 (parent 0700), no matter the umask, because the
// written token is a secret (configuration.md: 0600). The write is atomic:
// a temp file in the same directory is fsynced and renamed, so a crash
// never leaves a truncated credentials file.
func SaveCredentials(path string, c Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("credential directory %s: %w", dir, err)
	}
	data, err := jsonx.MarshalIndent(c, "  ")
	if err != nil {
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// O_CREATE honours the umask, so pin the mode explicitly.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	return nil
}
