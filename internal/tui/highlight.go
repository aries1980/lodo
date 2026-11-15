package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	propKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	propSepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	todoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	doingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	linkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Underline(true)
	refStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	headStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
)

var (
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	codeRe = regexp.MustCompile("`([^`\n]+)`")
	linkRe = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	refRe  = regexp.MustCompile(`\(\([0-9a-fA-F-]{36}\)\)`)
)

func highlightInline(s string) string {
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := boldRe.FindStringSubmatch(m)[1]
		return "**" + boldStyle.Render(inner) + "**"
	})
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := codeRe.FindStringSubmatch(m)[1]
		return "`" + codeStyle.Render(inner) + "`"
	})
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := linkRe.FindStringSubmatch(m)[1]
		return "[[" + linkStyle.Render(inner) + "]]"
	})
	s = refRe.ReplaceAllStringFunc(s, func(m string) string {
		return refStyle.Render(m)
	})
	return s
}

func Highlight(content string) string {
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# lodo-tui:") {
			b.WriteString(hintStyle.Render(line))
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
			b.WriteString(propKeyStyle.Render(key))
			b.WriteString(propSepStyle.Render("::"))
			if val != "" {
				b.WriteString(" " + highlightInline(val))
			}
			b.WriteByte('\n')
			continue
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(upper, "TODO "):
			i := strings.Index(line, "TODO")
			if i >= 0 {
				b.WriteString(line[:i] + todoStyle.Render("TODO") + highlightInline(line[i+4:]))
				b.WriteByte('\n')
				continue
			}
		case strings.HasPrefix(upper, "DOING "):
			i := strings.Index(strings.ToUpper(line), "DOING")
			if i >= 0 {
				b.WriteString(line[:i] + doingStyle.Render("DOING") + highlightInline(line[i+5:]))
				b.WriteByte('\n')
				continue
			}
		case strings.HasPrefix(upper, "DONE ") || strings.HasPrefix(upper, "CANCELLED "):
			b.WriteString(doneStyle.Render(highlightInline(line)))
			b.WriteByte('\n')
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			b.WriteString(headStyle.Render(highlightInline(line)))
			b.WriteByte('\n')
			continue
		}
		b.WriteString(highlightInline(line))
		b.WriteByte('\n')
	}
	out := b.String()
	return strings.TrimSuffix(out, "\n")
}
