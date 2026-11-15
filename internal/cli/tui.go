package cli

import (
	"context"
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

func runTUI(opts *options, ov configOverrides) error {
	cfg, sources, _, err := loadSources(opts, ov)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	if err := checkLogseqAvailable(context.Background(), sources, makeLogseqProbeNotify(opts, "tui")); err != nil {
		return &exitError{code: codeError, err: err}
	}
	ctx := context.Background()
	data, err := tui.Load(ctx, cfg, sources)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	reload := func() (tui.Data, error) {
		return tui.Load(context.Background(), cfg, sources)
	}
	if err := tui.RunProgram(data, reload); err != nil {
		return &exitError{code: codeError, err: err}
	}
	return nil
}
