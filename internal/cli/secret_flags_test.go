package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// Secret flags beat file and environment: a credentials file with an empty
// Logseq token still checks when --logseq-token carries the token, and a
// file with no google section still forecasts when the google flags carry
// the triple (against the fake, so no network).
func TestSecretFlagsOverrideFile(t *testing.T) {
	t.Setenv("LODO_LOGSEQ_TOKEN", "")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", "")
	t.Setenv("LODO_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("LODO_GOOGLE_REFRESH_TOKEN", "")

	api := miniApp(t)
	conf, cred := writeTestSetup(t, api)

	// Empty the file's Logseq token: without the flag this refuses.
	if err := os.WriteFile(cred, []byte(`{"logseq":{"token":""}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := &bytes.Buffer{}, &strings.Builder{}
	if code := Run([]string{"--config", conf, "check", "--logseq-token", "st-token"}, stdout, stderr); code != 0 {
		t.Fatalf("check with --logseq-token exit = %d, stderr %q", code, stderr.String())
	}

	// No google section in the file: flags supply the triple.
	if err := os.WriteFile(cred, []byte(`{"logseq":{"token":"st-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fg := &fakeGoogle{}
	base := fg.mount(t, `{"items": []}`)
	pointBoth(t, oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token"}, base)

	stdout, stderr = &bytes.Buffer{}, &strings.Builder{}
	args := []string{"--config", conf, "--json", "forecast",
		"--google-client-id", "cid",
		"--google-client-secret", "secret",
		"--google-refresh-token", "1//flag-refresh"}
	if code := Run(args, stdout, stderr); code != 0 {
		t.Fatalf("forecast with google flags exit = %d, stderr %q", code, stderr.String())
	}
	if got := fg.tokenSeen; len(got) != 1 || got[0] != "1//flag-refresh" {
		t.Fatalf("refresh token used = %q, want the flag value", got)
	}
}
