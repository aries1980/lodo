package cli

import (
	"io"
	"os"
)

// styler wraps human-readable text in ANSI colors when the destination
// wants them. Colors are enabled only for real terminals: pipes, cron,
// CI and tests (strings.Builder, bytes.Buffer) stay plain so output
// remains parseable and test assertions stay byte-stable.
//
// Disable order: --disable-colors, NO_COLOR env, TERM=dumb, non-terminal.
type styler struct {
	enabled bool
}

func newStyler(w io.Writer, disableColors bool) styler {
	if disableColors {
		return styler{}
	}
	if os.Getenv("NO_COLOR") != "" {
		return styler{}
	}
	if os.Getenv("TERM") == "dumb" {
		return styler{}
	}
	f, ok := w.(*os.File)
	if !ok {
		return styler{}
	}
	fi, err := f.Stat()
	if err != nil {
		return styler{}
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return styler{}
	}
	return styler{enabled: true}
}

// forceStyler is for tests that assert colored output without a TTY.
func forceStyler() styler { return styler{enabled: true} }

func (s styler) wrap(code, msg string) string {
	if !s.enabled {
		return msg
	}
	return "\x1b[" + code + "m" + msg + "\x1b[0m"
}

func (s styler) bold(msg string) string   { return s.wrap("1", msg) }
func (s styler) dim(msg string) string    { return s.wrap("2", msg) }
func (s styler) red(msg string) string    { return s.wrap("31", msg) }
func (s styler) green(msg string) string  { return s.wrap("32", msg) }
func (s styler) yellow(msg string) string { return s.wrap("33", msg) }
func (s styler) cyan(msg string) string   { return s.wrap("36", msg) }

func (s styler) header(msg string) string  { return s.bold(s.cyan(msg)) }
func (s styler) success(msg string) string { return s.green(msg) }
func (s styler) warning(msg string) string { return s.yellow(msg) }
func (s styler) danger(msg string) string  { return s.red(msg) }
