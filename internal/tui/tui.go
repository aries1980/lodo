package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aries1980/lodo/internal/theme"
)

func RunProgram(data Data, reload func() (Data, error), th theme.Theme) error {
	m := NewModel(data, reload, th)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
