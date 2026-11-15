package cli

import (
	"testing"
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

	e := mustE2E(t, e2eOptions{Google: true})

	// Empty the file's Logseq token: without the flag this refuses.
	if _, err := writeCredentials(t, e.Dir, `{"logseq":{"token":""}}`); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := e.Run("check", "--logseq-token", "st-token")
	if code != 0 {
		t.Fatalf("check with --logseq-token exit = %d, stderr %q", code, errOut)
	}

	// No google section in the file: flags supply the triple.
	if _, err := writeCredentials(t, e.Dir, e2eLogseqCreds); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = e.Run("--json", "forecast",
		"--google-client-id", "cid",
		"--google-client-secret", "secret",
		"--google-refresh-token", "1//flag-refresh")
	if code != 0 {
		t.Fatalf("forecast with google flags exit = %d, stderr %q", code, errOut)
	}
	if got := e.Calendar.TokenSeen(); len(got) != 1 || got[0] != "1//flag-refresh" {
		t.Fatalf("refresh token used = %q, want the flag value", got)
	}
}
