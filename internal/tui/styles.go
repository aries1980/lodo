package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/aries1980/lodo/internal/theme"
)

type styles struct {
	propKey lipgloss.Style
	propSep lipgloss.Style
	todo    lipgloss.Style
	doing   lipgloss.Style
	done    lipgloss.Style
	bold    lipgloss.Style
	code    lipgloss.Style
	link    lipgloss.Style
	ref     lipgloss.Style
	head    lipgloss.Style
	hint    lipgloss.Style
	title   lipgloss.Style
	dim     lipgloss.Style
	err     lipgloss.Style
	ok      lipgloss.Style
	select_ lipgloss.Style
}

func fg(s lipgloss.Style, color string) lipgloss.Style {
	if color == "" {
		return s
	}
	return s.Foreground(lipgloss.Color(color))
}

func stylesFor(th theme.Theme) styles {
	p := th.Palette
	if th.Concrete {
		return styles{}
	}
	return styles{
		propKey: fg(lipgloss.NewStyle().Bold(true), p.PropKey),
		propSep: fg(lipgloss.NewStyle(), p.PropSep),
		todo:    fg(lipgloss.NewStyle().Bold(true), p.Todo),
		doing:   fg(lipgloss.NewStyle().Bold(true), p.Doing),
		done:    fg(lipgloss.NewStyle(), p.Done),
		bold:    lipgloss.NewStyle().Bold(true),
		code:    fg(lipgloss.NewStyle(), p.Code),
		link:    fg(lipgloss.NewStyle().Underline(true), p.Link),
		ref:     fg(lipgloss.NewStyle(), p.Ref),
		head:    fg(lipgloss.NewStyle().Bold(true), p.Head),
		hint:    fg(lipgloss.NewStyle().Italic(true), p.Hint),
		title:   fg(lipgloss.NewStyle().Bold(true), p.Title),
		dim:     fg(lipgloss.NewStyle(), p.Dim),
		err:     fg(lipgloss.NewStyle().Bold(true), p.Err),
		ok:      fg(lipgloss.NewStyle(), p.Ok),
		select_: fg(lipgloss.NewStyle().Bold(true), p.Select),
	}
}
