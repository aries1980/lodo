package cli

import (
	"io"
	"os"

	"github.com/aries1980/lodo/internal/theme"
)

type styler struct {
	enabled bool
	th      theme.Theme
}

func newStyler(w io.Writer, disableColors bool, th theme.Theme) styler {
	if th.Concrete {
		return styler{th: th}
	}
	if disableColors {
		return styler{th: theme.For(theme.Concrete)}
	}
	if os.Getenv("NO_COLOR") != "" {
		return styler{th: theme.For(theme.Concrete)}
	}
	if os.Getenv("TERM") == "dumb" {
		return styler{th: theme.For(theme.Concrete)}
	}
	f, ok := w.(*os.File)
	if !ok {
		return styler{th: theme.For(theme.Concrete)}
	}
	fi, err := f.Stat()
	if err != nil {
		return styler{th: theme.For(theme.Concrete)}
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return styler{th: theme.For(theme.Concrete)}
	}
	return styler{enabled: true, th: th}
}

func forceStylerFor(th theme.Theme) styler {
	if th.Concrete {
		return styler{enabled: true, th: theme.For(theme.Monokai)}
	}
	return styler{enabled: true, th: th}
}

func forceStyler() styler { return forceStylerFor(theme.For(theme.Monokai)) }

func (s styler) wrap(code, msg string) string {
	if !s.enabled {
		return msg
	}
	if code == "" {
		return msg
	}
	return "\x1b[" + code + "m" + msg + "\x1b[0m"
}

func (s styler) bold(msg string) string   { return s.wrap(s.th.Bold, msg) }
func (s styler) dim(msg string) string    { return s.wrap(s.th.Dim, msg) }
func (s styler) red(msg string) string    { return s.wrap(s.th.Red, msg) }
func (s styler) green(msg string) string  { return s.wrap(s.th.Green, msg) }
func (s styler) yellow(msg string) string { return s.wrap(s.th.Yellow, msg) }
func (s styler) cyan(msg string) string   { return s.wrap(s.th.Cyan, msg) }

func (s styler) header(msg string) string  { return s.bold(s.cyan(msg)) }
func (s styler) success(msg string) string { return s.green(msg) }
func (s styler) warning(msg string) string { return s.yellow(msg) }
func (s styler) danger(msg string) string  { return s.red(msg) }
