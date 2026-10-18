package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/config"
)

func credTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "lodo", "credentials.json")
}

func writeCreds(t *testing.T, path string, mode os.FileMode, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCredentials(t *testing.T) {
	path := credTestPath(t)
	writeCreds(t, path, 0o600, "{\n  \"logseq\": {\"token\": \"kqz3v6g,\"},\n  \"google\": {\"refresh_token\": \"1//xyz\"}\n}")

	c, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if c.Logseq.Token != "kqz3v6g," {
		t.Fatalf("token = %q, want the file's token", c.Logseq.Token)
	}
}

func TestCredentialsMissingFile(t *testing.T) {
	_, err := config.LoadCredentials(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("LoadCredentials succeeded for a missing file; forecast must refuse without a token")
	}
	if !strings.Contains(err.Error(), "credentials file") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want the missing-file explanation", err.Error())
	}
}

func TestCredentialsRefuseLoosePermissions(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{"world readable", 0o644},
		{"group readable", 0o640},
		{"group writable", 0o660},
		{"wide open", 0o666},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := credTestPath(t)
			writeCreds(t, path, tt.mode, `{"logseq":{"token":"t"}}`)
			_, err := config.LoadCredentials(path)
			if err == nil {
				t.Fatalf("LoadCredentials accepted mode %04o", tt.mode)
			}
			if !strings.Contains(err.Error(), "0600") {
				t.Fatalf("error = %q, want it to name the 0600 rule", err.Error())
			}
		})
	}
}

func TestCredentialsMalformedJSON(t *testing.T) {
	path := credTestPath(t)
	writeCreds(t, path, 0o600, "not json")
	if _, err := config.LoadCredentials(path); err == nil {
		t.Fatal("LoadCredentials accepted malformed JSON")
	}
}

func TestCredentialsEmptyToken(t *testing.T) {
	path := credTestPath(t)
	writeCreds(t, path, 0o600, `{"logseq":{"token": ""}}`)
	_, err := config.LoadCredentials(path)
	if err == nil || !strings.Contains(err.Error(), "logseq.token is empty") {
		t.Fatalf("error = %v, want the empty-token message", err)
	}
}

func TestSaveCredentialsWrites0600(t *testing.T) {
	path := credTestPath(t)

	in := config.Credentials{Logseq: config.LogseqCredentials{Token: "tok"}}
	if err := config.SaveCredentials(path, in); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("saved mode = %04o, want 0600 (the umask must not loosen it)", got)
	}

	out, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if diff := cmp.Diff(in, out); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}

	// The on-disk shape matches the configuration.md example.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var structured struct {
		Logseq struct {
			Token string `json:"token"`
		} `json:"logseq"`
	}
	if err := json.Unmarshal(raw, &structured); err != nil {
		t.Fatalf("saved file is not the documented JSON shape: %v (%s)", err, raw)
	}
	if structured.Logseq.Token != "tok" {
		t.Fatalf("saved token = %q", structured.Logseq.Token)
	}
}

func TestSaveCredentialsOverwritesModeAndContent(t *testing.T) {
	path := credTestPath(t)
	// A pre-existing loose file gets tightened rather than refused: saving
	// fixes the permissions it owns.
	writeCreds(t, path, 0o644, "stale")
	in := config.Credentials{Logseq: config.LogseqCredentials{Token: "fresh"}}
	if err := config.SaveCredentials(path, in); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode after save = %04o, want 0600", got)
	}
	out, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if out.Logseq.Token != "fresh" {
		t.Fatalf("token = %q, want the saved one", out.Logseq.Token)
	}
}

func TestDefaultPathsUnderXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg-conf")
	t.Setenv("XDG_STATE_HOME", "/xdg-state")
	t.Setenv("HOME", "/home")

	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if cfgPath != "/xdg-conf/lodo/config.yaml" {
		t.Fatalf("config path = %q, want $XDG_CONFIG_HOME/lodo/config.yaml", cfgPath)
	}

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if credPath != "/xdg-state/lodo/credentials.json" {
		t.Fatalf("credentials path = %q, want $XDG_STATE_HOME/lodo/credentials.json", credPath)
	}
}

func TestDefaultPathsFallBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home")

	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if cfgPath != "/home/.config/lodo/config.yaml" {
		t.Fatalf("config path = %q, want ~/.config/lodo/config.yaml", cfgPath)
	}

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if credPath != "/home/.local/state/lodo/credentials.json" {
		t.Fatalf("credentials path = %q, want ~/.local/state/lodo/credentials.json", credPath)
	}
}

func clearCredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LODO_LOGSEQ_TOKEN", "")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", "")
	t.Setenv("LODO_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("LODO_GOOGLE_REFRESH_TOKEN", "")
}

func TestReadCredentialsEnvOverlayWinsPerField(t *testing.T) {
	clearCredEnv(t)
	path := credTestPath(t)
	writeCreds(t, path, 0o600, `{"logseq":{"token":"file-token"},"google":{"client_id":"file-id","client_secret":"file-secret","refresh_token":"file-refresh"}}`)

	t.Setenv("LODO_LOGSEQ_TOKEN", " env-token ")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", "env-id")
	t.Setenv("LODO_GOOGLE_REFRESH_TOKEN", "env-refresh")

	c, err := config.ReadCredentials(path)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if c.Logseq.Token != "env-token" {
		t.Fatalf("logseq token = %q, want trimmed env value", c.Logseq.Token)
	}
	if c.Google.ClientID != "env-id" {
		t.Fatalf("client_id = %q, want env", c.Google.ClientID)
	}
	if c.Google.ClientSecret != "file-secret" {
		t.Fatalf("client_secret = %q, want file value kept when env empty", c.Google.ClientSecret)
	}
	if c.Google.RefreshToken != "env-refresh" {
		t.Fatalf("refresh_token = %q, want env", c.Google.RefreshToken)
	}
}

func TestReadCredentialsMissingFileWithEnv(t *testing.T) {
	clearCredEnv(t)
	missing := filepath.Join(t.TempDir(), "absent.json")
	t.Setenv("LODO_LOGSEQ_TOKEN", "env-token")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", "env-id")
	t.Setenv("LODO_GOOGLE_CLIENT_SECRET", "env-secret")

	c, err := config.ReadCredentials(missing)
	if err != nil {
		t.Fatalf("ReadCredentials with env: %v", err)
	}
	if c.Logseq.Token != "env-token" {
		t.Fatalf("logseq token = %q, want env", c.Logseq.Token)
	}
	if c.Google == nil || c.Google.ClientID != "env-id" || c.Google.ClientSecret != "env-secret" {
		t.Fatalf("google = %+v, want env pair", c.Google)
	}

	loaded, err := config.LoadCredentials(missing)
	if err != nil {
		t.Fatalf("LoadCredentials with env: %v", err)
	}
	if loaded.Logseq.Token != "env-token" {
		t.Fatalf("loaded token = %q, want env", loaded.Logseq.Token)
	}
}

func TestReadCredentialsMissingFileWithoutEnvStillRefuses(t *testing.T) {
	clearCredEnv(t)
	missing := filepath.Join(t.TempDir(), "absent.json")
	if _, err := config.ReadCredentials(missing); err == nil {
		t.Fatal("ReadCredentials succeeded with no file and no env")
	}
}

func TestWithCredentialOverridesFlagWinsOverEnvAndFile(t *testing.T) {
	clearCredEnv(t)
	path := credTestPath(t)
	writeCreds(t, path, 0o600, `{"logseq":{"token":"file-token"},"google":{"client_id":"file-id","client_secret":"file-secret","refresh_token":"file-refresh"}}`)

	t.Setenv("LODO_LOGSEQ_TOKEN", "env-token")
	t.Setenv("LODO_GOOGLE_CLIENT_ID", "env-id")
	t.Setenv("LODO_GOOGLE_CLIENT_SECRET", "env-secret")
	t.Setenv("LODO_GOOGLE_REFRESH_TOKEN", "env-refresh")

	c, err := config.ReadCredentials(path)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	got := c.WithCredentialOverrides(config.CredentialOverrides{
		LogseqToken:        " flag-token ",
		GoogleClientID:     "flag-id",
		GoogleRefreshToken: "flag-refresh",
	})
	if got.Logseq.Token != "flag-token" {
		t.Fatalf("logseq token = %q, want trimmed flag", got.Logseq.Token)
	}
	if got.Google.ClientID != "flag-id" {
		t.Fatalf("client_id = %q, want flag", got.Google.ClientID)
	}
	if got.Google.ClientSecret != "env-secret" {
		t.Fatalf("client_secret = %q, want env kept when flag empty", got.Google.ClientSecret)
	}
	if got.Google.RefreshToken != "flag-refresh" {
		t.Fatalf("refresh_token = %q, want flag", got.Google.RefreshToken)
	}

	// Empty overrides keep file+env.
	kept := c.WithCredentialOverrides(config.CredentialOverrides{})
	if kept.Logseq.Token != "env-token" || kept.Google.ClientID != "env-id" {
		t.Fatalf("empty overrides changed credentials: %+v", kept)
	}
}
