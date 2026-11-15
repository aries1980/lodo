package cli_test

import (
	"strings"
	"testing"

	"github.com/aries1980/lodo/internal/cli"
	"github.com/aries1980/lodo/internal/i18n"
)

func TestHelpTranslationsVisible(t *testing.T) {
	i18n.SetLang(i18n.LangEnUS)
	t.Cleanup(func() { i18n.SetLang(i18n.Default) })

	enUS := i18n.T("disable colors in human-readable output (also NO_COLOR, TERM=dumb, non-TTY)")
	if enUS != "disable colors in human-readable output (also NO_COLOR, TERM=dumb, non-TTY)" {
		t.Fatalf("en-US lookup failed: %q", enUS)
	}

	i18n.SetLang(i18n.LangEnGB)
	got := i18n.T("disable colors in human-readable output (also NO_COLOR, TERM=dumb, non-TTY)")
	if got != "disable colours in human-readable output (also NO_COLOR, TERM=dumb, non-TTY)" {
		t.Fatalf("en-GB colours translation missing: %q", got)
	}

	for _, msgid := range []string{
		"Schedule Logseq tasks onto one Google Calendar",
		"machine-readable output",
		"Validate the graph and local state; prompts before any write",
		"Print the Schedule that would be written; a dry run",
		"select language (also LODO_LANG, LANG, LC_ALL, LC_MESSAGES)",
	} {
		if i18n.T(msgid) == "" {
			t.Fatalf("msgid %q has empty translation", msgid)
		}
	}
}

func runHelp(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	// Ensure locale env does not leak from the host into the assertion.
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if _, ok := env[k]; !ok {
			t.Setenv(k, "")
		}
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	code := cli.Run(args, stdout, stderr)
	if code != 0 {
		t.Fatalf("help exit = %d, stderr = %q", code, stderr.String())
	}
	return stdout.String()
}

func TestHelpLangFlag(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.Default) })

	outUS := runHelp(t, nil, "--help")
	if !strings.Contains(outUS, "disable colors") {
		t.Fatalf("en-US --help missing colors:\n%s", outUS)
	}

	outGB := runHelp(t, nil, "--lang", "en-GB", "--help")
	if !strings.Contains(outGB, "disable colours") {
		t.Fatalf("--lang en-GB --help missing colours:\n%s", outGB)
	}

	outEq := runHelp(t, nil, "--lang=en-GB", "--help")
	if !strings.Contains(outEq, "disable colours") {
		t.Fatalf("--lang=en-GB --help missing colours:\n%s", outEq)
	}

	outEnv := runHelp(t, map[string]string{"LODO_LANG": "en-GB"}, "--help")
	if !strings.Contains(outEnv, "disable colours") {
		t.Fatalf("LODO_LANG=en-GB --help missing colours:\n%s", outEnv)
	}
}
