package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultConfigPath returns the config.yaml location: config.yaml lives
// outside the graph (ADR 0005), under
// ${XDG_CONFIG_HOME:-~/.config}/lodo/config.yaml.
func DefaultConfigPath() (string, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lodo", "config.yaml"), nil
}

// DefaultCredentialsPath returns the credentials.json location:
// ${XDG_STATE_HOME:-~/.local/state}/lodo/credentials.json. Both state files
// live under ~/.local/state/lodo so deleting that one directory resets Lodo
// without touching notes or config (configuration.md).
func DefaultCredentialsPath() (string, error) {
	dir, err := xdgDir("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lodo", "credentials.json"), nil
}

// DefaultStorePath returns the lodo.db location:
// ${XDG_STATE_HOME:-~/.local/state}/lodo/lodo.db. The store is the system of
// record (ADR 0001); it lives beside credentials.json so deleting that one
// directory resets Lodo without touching notes or config (configuration.md).
func DefaultStorePath() (string, error) {
	dir, err := xdgDir("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lodo", "lodo.db"), nil
}

// xdgDir resolves one XDG variable against the home directory fallback.
func xdgDir(envVar, fallback string) (string, error) {
	if v := os.Getenv(envVar); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory: %w", err)
	}
	return filepath.Join(home, fallback), nil
}
