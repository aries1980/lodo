package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func RunProgram(data Data, reload func() (Data, error)) error {
	m := NewModel(data, reload)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
