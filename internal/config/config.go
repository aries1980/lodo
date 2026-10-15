// Package config loads Lodo's configuration and credentials (ADR 0005): the
// graph holds user intent about work, the config file holds how Lodo runs,
// and everything secret lives in the state directory.
//
// The deciding rule for any future setting: secret or machine-bound → file;
// user intent about work → graph.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aries1980/lodo/internal/parse"
)

// Config mirrors config.yaml.
type Config struct {
	Logseq     Logseq     `json:"logseq"`
	Scheduling Scheduling `json:"scheduling"`
	Calendar   Calendar   `json:"calendar"`
}

// Logseq is the graph source: where Lodo reads over the HTTP API (ADR 0002).
type Logseq struct {
	// APIURL is the Logseq app's HTTP API endpoint, e.g.
	// http://127.0.0.1:12315/api.
	APIURL string
	// TimeMapsPage names the page holding the Time Maps; default "Time Maps".
	TimeMapsPage string
	// Insecure skips TLS verification for an app serving a self-signed
	// certificate. It is machine-bound, so it lives in the file (ADR 0005),
	// never as a flag: whoever wrote the config accepted that certificate.
	Insecure bool
}

// Scheduling settings.
type Scheduling struct {
	// Granularity is the calendar's quantum, a whole number of minutes that
	// divides 60 (15m, 20m, 30m), and the floor for small Tasks.
	Granularity string
	// WindowDays is the rolling Scheduling window; nothing beyond it is
	// scheduled or synced.
	WindowDays int
	// Timezone names the zone Time Map hours are interpreted in: Time Map
	// hours are absolute local times, so this is what "09:00" means.
	Timezone string
}

// Calendar is the single output Calendar.
type Calendar struct {
	// Provider is "google" in v0.1.
	Provider string
	// ID selects the one Google Calendar; default "primary".
	ID string
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

// rawFile mirrors the YAML document before validation.
type rawFile struct {
	Logseq struct {
		APIURL       string `yaml:"api_url"`
		TimeMapsPage string `yaml:"time_maps_page"`
		Insecure     bool   `yaml:"insecure"`
	} `yaml:"logseq"`
	Scheduling struct {
		Granularity string `yaml:"granularity"`
		WindowDays  int    `yaml:"window_days"`
		Timezone    string `yaml:"timezone"`
	} `yaml:"scheduling"`
	Calendar struct {
		Provider string `yaml:"provider"`
		ID       string `yaml:"id"`
	} `yaml:"calendar"`
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config file %s: %w (pass --config to override; default %s)", path, err, DefaultConfig)
	}

	var raw rawFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("config file %s: %w", path, err)
	}

	cfg := Config{
		Logseq: Logseq{
			APIURL:       strings.TrimSpace(raw.Logseq.APIURL),
			TimeMapsPage: raw.Logseq.TimeMapsPage,
			Insecure:     raw.Logseq.Insecure,
		},
		Scheduling: Scheduling{
			Granularity: raw.Scheduling.Granularity,
			WindowDays:  raw.Scheduling.WindowDays,
			Timezone:    strings.TrimSpace(raw.Scheduling.Timezone),
		},
		Calendar: Calendar{
			Provider: strings.TrimSpace(raw.Calendar.Provider),
			ID:       strings.TrimSpace(raw.Calendar.ID),
		},
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

func validate(cfg Config) (Config, error) {
	if cfg.Logseq.APIURL == "" {
		return Config{}, fmt.Errorf("logseq.api_url: required")
	}
	u, err := url.Parse(cfg.Logseq.APIURL)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return Config{}, fmt.Errorf("logseq.api_url %q: must be an absolute http(s) URL", cfg.Logseq.APIURL)
	}
	if cfg.Logseq.TimeMapsPage == "" {
		cfg.Logseq.TimeMapsPage = DefaultTimeMapsPage
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
	return cfg, nil
}

// Overrides are one-run CLI overrides (configuration.md): any setting may be
// overridden for a single run by a flag; overrides are never persisted.
type Overrides struct {
	// WindowDays overrides scheduling.window_days; 0 keeps the config value.
	WindowDays int
	// Timezone overrides scheduling.timezone; "" keeps the config value.
	Timezone string
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
	return cfg, nil
}

// Credentials is the contents of credentials.json: the logseq section that
// Lodo owns for the loop commands (M3), and the google section that the
// OAuth flow fills in (calendar.md Auth). Unknown sections are tolerated so
// adding one does not invalidate the file.
type Credentials struct {
	Logseq LogseqCredentials  `json:"logseq"`
	Google *GoogleCredentials `json:"google,omitempty"`
}

// LogseqCredentials carries the Logseq HTTP API token.
type LogseqCredentials struct {
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

// GoogleSetup returns the google section as `lodo auth google` needs it:
// the client pair, still without demanding a refresh token. Missing
// entries are refusals with the fix, never prompts.
func (c Credentials) GoogleSetup() (GoogleCredentials, error) {
	var zero GoogleCredentials
	if c.Google == nil {
		return zero, fmt.Errorf(
			"google credentials: missing (create an OAuth client with redirect URI http://127.0.0.1/ and paste {\"google\":{\"client_id\":\"…\",\"client_secret\":\"…\"}} into %s)", DefaultCredentials)
	}
	g := *c.Google
	if g.ClientID == "" || g.ClientSecret == "" {
		return zero, fmt.Errorf(
			"google credentials: client_id and client_secret are required; paste them into %s from the OAuth client with redirect URI http://127.0.0.1/", DefaultCredentials)
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
			"google credentials: refresh_token is empty; run `lodo auth google` once to store it in %s", DefaultCredentials)
	}
	return g, nil
}

// ReadCredentials reads credentials.json at path. The file must be 0600: a
// credentials file readable by group or others is a refusal, not a warning.
// Nothing else is demanded here: which section is required is the caller's
// call — the loop commands need a Logseq token, `auth google` needs the
// Google client pair, and both live in the same file.
func ReadCredentials(path string) (Credentials, error) {
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
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("credentials file %s: %w", path, err)
	}
	return c, nil
}

// LoadCredentials reads credentials.json at path for the loop commands: the
// 0600 file plus a non-empty Logseq API token. The google section travels
// along as it is written; the Calendar side validates its own completeness.
func LoadCredentials(path string) (Credentials, error) {
	c, err := ReadCredentials(path)
	if err != nil {
		return Credentials{}, err
	}
	if c.Logseq.Token == "" {
		return Credentials{}, fmt.Errorf("credentials file %s: logseq.token is empty; paste the Logseq API token from the app (Settings → Integrations)", path)
	}
	return c, nil
}

// SaveCredentials writes c to path, creating the state directory if needed.
// The file is written 0600 (parent 0700), no matter the umask, because the
// written token is a secret (configuration.md: 0600).
func SaveCredentials(path string, c Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("credential directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	// O_CREATE honours the umask, so pin the mode explicitly.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("credentials file %s: %w", path, err)
	}
	return nil
}
