package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/aries1980/lodo/internal/tui"
)

var ttyCheck = func() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func runTUI(ctx context.Context, opts *options, ov configOverrides) error {
	cfg, sources, _, err := loadSources(opts, ov)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	th, warn := resolveTheme(cfg, opts.config)
	if warn != "" {
		_, _ = fmt.Fprintln(errWriter(opts), warn)
	}
	if err := checkLogseqAvailable(ctx, sources, makeLogseqProbeNotify(opts, "tui", th)); err != nil {
		return &exitError{code: codeError, err: err}
	}
	data, err := tui.Load(ctx, cfg, sources)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	reload := func() (tui.Data, error) {
		return tui.Load(ctx, cfg, sources)
	}
	if err := tui.RunProgram(data, reload, th); err != nil {
		return &exitError{code: codeError, err: err}
	}
	return nil
}
