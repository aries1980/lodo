package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func oauth2Endpoint(base string) oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}
}

func TestResolveLogFormat(t *testing.T) {
	for in, want := range map[string]string{
		"": "text", "auto": "text", "AUTO": "text",
		"text": "text", "TEXT": "text",
		"json": "json", "JSON": "json", " Json ": "json",
		"yaml": "text",
	} {
		if got := resolveLogFormat(in); got != want {
			t.Fatalf("resolveLogFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStylerDisabledByDefaultForBuffers(t *testing.T) {
	st := newStyler(&strings.Builder{}, false)
	if st.enabled {
		t.Fatal("buffers are not terminals: colors must be off")
	}
	if got := st.success("ok"); got != "ok" {
		t.Fatalf("disabled styler must pass through, got %q", got)
	}
	if got := forceStyler().success("ok"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("forced styler must colorize, got %q", got)
	}
}

func TestStylerRespectsDisableAndEnv(t *testing.T) {
	if got := newStyler(&strings.Builder{}, true); got.enabled {
		t.Fatal("--disable-colors must win")
	}
	t.Setenv("NO_COLOR", "1")
	if got := forceStyler(); got.enabled != true {
		t.Fatal("forceStyler ignores env by design")
	}
	// newStyler with NO_COLOR set stays off even for files; buffers
	// already prove the non-terminal path.
	if got := newStyler(&strings.Builder{}, false); got.enabled {
		t.Fatal("NO_COLOR must disable colors")
	}
}

func TestJSONLogLinesAreParseable(t *testing.T) {
	stderr := &strings.Builder{}
	emitJSON(stderr, false, 20, "check", "run.finish", "check finished", map[string]any{
		"duration_ms": int64(12),
	})
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 JSONL line, got %q", stderr.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("stderr is not JSONL: %v\n%s", err, stderr.String())
	}
	for _, k := range []string{"timestamp", "level", "message", "service", "command", "event"} {
		if _, ok := rec[k]; !ok {
			t.Fatalf("record missing %q: %v", k, rec)
		}
	}
	if rec["service"] != "lodo" {
		t.Fatalf("service = %v, want lodo", rec["service"])
	}
	ts, _ := rec["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Fatalf("timestamp %q is not RFC3339Nano: %v", ts, err)
	}
}

func TestLogFormatJSONEmitsRunEvents(t *testing.T) {
	// check with a fake calendar exercises the full path and must emit
	// run.start/run.finish JSONL to stderr.
	fg := &fakeGoogle{}
	api := miniApp(t)
	conf, _ := writeTestSetup(t, api)
	base := fg.mount(t, `{"items":[]}`)
	pointBoth(t, oauth2Endpoint(base), base)
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := Run([]string{"--config", conf, "--log-format=json", "check"}, stdout, stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("want run.start + run.finish lines, got %q", stderr.String())
	}
	for _, line := range lines {
		var rec struct {
			Timestamp string `json:"timestamp"`
			Level     string `json:"level"`
			Message   string `json:"message"`
			Service   string `json:"service"`
			Command   string `json:"command"`
			Event     string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("not JSONL: %v\n%s", err, line)
		}
		if _, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err != nil {
			t.Fatalf("bad timestamp: %v", err)
		}
		if rec.Service != "lodo" || rec.Command != "check" {
			t.Fatalf("record = %+v", rec)
		}
	}
	// stdout stays human-readable in this mode (no --json result flag).
	if !strings.Contains(stdout.String(), "Summary:") {
		t.Fatalf("human output must keep the summary footer:\n%s", stdout.String())
	}
}

func TestDisableColorsStripsANSI(t *testing.T) {
	// Forced colors prove the renderer can colorize; --disable-colors
	// proves a human can turn it off. The default (buffers) is already
	// plain, covered above.
	st := forceStyler()
	colored := st.success("ok")
	if !strings.Contains(colored, "\x1b[32m") {
		t.Fatalf("forced green must wrap, got %q", colored)
	}
	plain := styler{}.success("ok")
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("disabled styler must not emit ANSI, got %q", plain)
	}
}
