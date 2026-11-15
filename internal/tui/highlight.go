package tui

import (
	"regexp"
	"strings"

	"github.com/aries1980/lodo/internal/theme"
)

var (
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	codeRe = regexp.MustCompile("`([^`\n]+)`")
	linkRe = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	refRe  = regexp.MustCompile(`\(\([0-9a-fA-F-]{36}\)\)`)
)

func highlightInline(s string, st styles) string {
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := boldRe.FindStringSubmatch(m)[1]
		return "**" + st.bold.Render(inner) + "**"
	})
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := codeRe.FindStringSubmatch(m)[1]
		return "`" + st.code.Render(inner) + "`"
	})
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := linkRe.FindStringSubmatch(m)[1]
		return "[[" + st.link.Render(inner) + "]]"
	})
	s = refRe.ReplaceAllStringFunc(s, func(m string) string {
		return st.ref.Render(m)
	})
	return s
}

func Highlight(content string, th theme.Theme) string {
	st := stylesFor(th)
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# lodo-tui:") {
			b.WriteString(st.hint.Render(line))
			b.WriteByte('\n')
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		lead := line[:len(line)-len(trimmed)]
		if isPropLine(trimmed) {
			idx := strings.Index(trimmed, "::")
			key := strings.TrimSpace(trimmed[:idx])
			val := ""
			if idx+2 < len(trimmed) {
				val = strings.TrimSpace(trimmed[idx+2:])
			}
			b.WriteString(lead)
			b.WriteString(st.propKey.Render(key))
			b.WriteString(st.propSep.Render("::"))
			if val != "" {
				b.WriteString(" " + highlightInline(val, st))
			}
			b.WriteByte('\n')
			continue
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "TODO "):
			i := strings.Index(line, "TODO")
			if i >= 0 {
				b.WriteString(line[:i] + st.todo.Render("TODO") + highlightInline(line[i+4:], st))
				b.WriteByte('\n')
				continue
			}
		case strings.HasPrefix(upper, "DOING "):
			i := strings.Index(strings.ToUpper(line), "DOING")
			if i >= 0 {
				b.WriteString(line[:i] + st.doing.Render("DOING") + highlightInline(line[i+5:], st))
				b.WriteByte('\n')
				continue
			}
		case strings.HasPrefix(upper, "DONE ") || strings.HasPrefix(upper, "CANCELLED "):
			b.WriteString(st.done.Render(highlightInline(line, st)))
			b.WriteByte('\n')
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			b.WriteString(st.head.Render(highlightInline(line, st)))
			b.WriteByte('\n')
			continue
		}
		b.WriteString(highlightInline(line, st))
		b.WriteByte('\n')
	}
	out := b.String()
	return strings.TrimSuffix(out, "\n")
}
