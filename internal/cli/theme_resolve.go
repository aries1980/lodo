package cli

import (
	"path/filepath"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/theme"
)

func resolveTheme(cfg config.Config, configPath string) (theme.Theme, string) {
	dir := ""
	if configPath != "" {
		dir = filepath.Dir(configPath)
	} else if def, err := config.DefaultConfigPath(); err == nil {
		dir = filepath.Dir(def)
	}
	return theme.Resolve(cfg.UI.Theme, dir)
}
