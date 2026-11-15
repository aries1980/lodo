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

func runTUI(ctx context.Context, opts *options, ov configOverrides) error {
	cmd, err := prepare(ctx, opts, ov, false, "tui")
	if err != nil {
		return fail(err)
	}
	defer cmd.close()

	data, err := tui.Load(ctx, cmd.cfg, cmd.sources)
	if err != nil {
		return fail(err)
	}
	reload := func() (tui.Data, error) {
		return tui.Load(ctx, cmd.cfg, cmd.sources)
	}
	if err := tui.RunProgram(data, reload, cmd.theme); err != nil {
		return fail(err)
	}
	cmd.finish(nil)
	return nil
}
