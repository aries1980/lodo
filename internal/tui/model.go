package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/theme"
)

type state int

const (
	stateList state = iota
	stateView
	stateInvalid
	stateFixMap
	stateSaving
)

type editorFinishedMsg struct {
	id   string
	path string
	err  error
}

type saveFinishedMsg struct {
	id      string
	content string
	err     error
}

type reloadedMsg struct {
	data Data
	err  error
}

type Model struct {
	data          Data
	reload        func() (Data, error)
	table         table.Model
	viewport      viewport.Model
	state         state
	width         int
	height        int
	selectedID    string
	selectedTask  model.Task
	editedContent string
	issues        []resolve.Issue
	status        string
	fixOptions    []string
	fixValues     []string
	fixCursor     int
	fixForID      string
	pendingTmp    string
	th            theme.Theme
	st            styles
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	s = strings.TrimSuffix(s, "0m")
	if s == "" {
		return d.String()
	}
	return s
}

func formatTaskRow(t model.Task) table.Row {
	dur := formatDuration(t.Duration)
	maps := "(default)"
	if len(t.TimeMaps) > 0 {
		maps = strings.Join(t.TimeMaps, ", ")
	}
	pri := "—"
	if t.Priority != "" {
		pri = string(t.Priority)
	}
	dead := "—"
	if t.Deadline != nil {
		dead = t.Deadline.String()
	}
	graph := t.Graph
	if graph == "" {
		graph = "—"
	}
	return table.Row{t.Text, graph, dur, maps, pri, dead}
}

func buildTable(tasks []model.Task, th theme.Theme) table.Model {
	cols := []table.Column{
		{Title: "Task", Width: 44},
		{Title: "Graph", Width: 10},
		{Title: "Duration", Width: 10},
		{Title: "Time Maps", Width: 22},
		{Title: "Pri", Width: 5},
		{Title: "Deadline", Width: 12},
	}
	rows := make([]table.Row, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, formatTaskRow(t))
	}
	tb := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	s := table.DefaultStyles()
	border := "8"
	selFg := "15"
	selBg := "8"
	if !th.Concrete {
		if th.Palette.TableBorder != "" {
			border = th.Palette.TableBorder
		}
		if th.Palette.TableSelFg != "" {
			selFg = th.Palette.TableSelFg
		}
		if th.Palette.TableSelBg != "" {
			selBg = th.Palette.TableSelBg
		}
	}
	s.Header = s.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).BorderBottom(true).Bold(true)
	if th.Concrete {
		s.Selected = s.Selected.Bold(false)
	} else {
		s.Selected = s.Selected.Foreground(lipgloss.Color(selFg)).Background(lipgloss.Color(selBg)).Bold(false)
	}
	tb.SetStyles(s)
	return tb
}

func NewModel(data Data, reload func() (Data, error), th theme.Theme) Model {
	tb := buildTable(data.Tasks, th)
	vp := viewport.New(80, 20)
	return Model{data: data, reload: reload, table: tb, viewport: vp, state: stateList, width: 80, height: 24, th: th, st: stylesFor(th)}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) taskAtCursor() (model.Task, string, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.data.Tasks) {
		return model.Task{}, "", false
	}
	t := m.data.Tasks[idx]
	return t, t.ID, true
}

func (m Model) rawContent(id string) string {
	if b, ok := m.data.Blocks[id]; ok && b != nil {
		return b.Content
	}
	return ""
}

func footerFor(id string, data *Data) string {
	var sb strings.Builder
	for _, t := range data.Tasks {
		if t.ID != id {
			continue
		}
		sb.WriteString("\n-- resolved --\n")
		sb.WriteString("duration: " + formatDuration(t.Duration) + "\n")
		maps := "(global default)"
		if len(t.TimeMaps) > 0 {
			maps = strings.Join(t.TimeMaps, ", ")
		}
		sb.WriteString("time-map: " + maps + "\n")
		if t.Priority != "" {
			sb.WriteString("priority: " + string(t.Priority) + "\n")
		}
		if t.Deadline != nil {
			sb.WriteString("deadline: " + t.Deadline.String() + "\n")
		}
		if p, ok := data.Provenance[id]; ok {
			for _, pp := range p.Properties {
				if pp.Inherited {
					fmt.Fprintf(&sb, "%s: %s (inherited %s %s)\n", pp.Property, pp.Value, pp.Source, pp.SourceID)
				}
			}
		}
	}
	return sb.String()
}

func editorCmd(id, content string) (string, tea.Cmd) {
	tmp, err := os.CreateTemp("", "lodo-*.md")
	if err != nil {
		return "", func() tea.Msg {
			return editorFinishedMsg{id: id, err: err}
		}
	}
	hint := "# lodo-tui: edit full Logseq block, save+quit to validate. Lines starting with '# lodo-tui:' are stripped.\n"
	if _, err := tmp.WriteString(content + "\n\n" + hint); err != nil {
		_ = tmp.Close()
		return tmp.Name(), func() tea.Msg {
			return editorFinishedMsg{id: id, path: tmp.Name(), err: err}
		}
	}
	_ = tmp.Close()
	path := tmp.Name()
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c", editor+" \"$1\"", "lodo-tui-sh", path)
	return path, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{id: id, path: path, err: err}
	})
}

func saveCmd(data Data, id, content string) tea.Cmd {
	return func() tea.Msg {
		graph := data.GraphOf[id]
		var found bool
		for _, s := range data.Sources {
			if s.Name == graph {
				if err := s.Client.UpdateBlock(context.Background(), id, stripHintLines(content)); err != nil {
					return saveFinishedMsg{id: id, content: content, err: err}
				}
				found = true
				break
			}
		}
		if !found {
			return saveFinishedMsg{id: id, content: content, err: fmt.Errorf("no Logseq source for graph %q", graph)}
		}
		return saveFinishedMsg{id: id, content: content}
	}
}

func reloadCmd(reload func() (Data, error)) tea.Cmd {
	return func() tea.Msg {
		d, err := reload()
		return reloadedMsg{data: d, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width)
		h := msg.Height - 6
		if h < 5 {
			h = 5
		}
		m.table.SetHeight(h)
		m.viewport.Width = msg.Width
		m.viewport.Height = h
		return m, nil
	case editorFinishedMsg:
		m.pendingTmp = ""
		if msg.err != nil {
			m.state = stateInvalid
			m.selectedID = msg.id
			m.issues = []resolve.Issue{{Kind: "editor", Context: "editor", Detail: msg.err.Error()}}
			m.status = "editor failed: " + msg.err.Error()
			return m, nil
		}
		raw, err := os.ReadFile(msg.path)
		_ = os.Remove(msg.path)
		if err != nil {
			m.state = stateInvalid
			m.selectedID = msg.id
			m.status = "cannot read temp file: " + err.Error()
			return m, nil
		}
		edited := string(raw)
		m.editedContent = stripHintLines(edited)
		_, issues, ok := ValidateEdited(&m.data, msg.id, edited)
		if ok {
			m.state = stateSaving
			m.selectedID = msg.id
			m.status = "saving to Logseq…"
			return m, saveCmd(m.data, msg.id, m.editedContent)
		}
		m.state = stateInvalid
		m.selectedID = msg.id
		m.issues = issues
		m.viewport.SetContent(Highlight(m.editedContent, m.th) + "\n\n" + issuesText(issues, m.st))
		m.viewport.GotoTop()
		return m, nil
	case saveFinishedMsg:
		if msg.err != nil {
			m.state = stateInvalid
			m.selectedID = msg.id
			m.editedContent = msg.content
			m.issues = []resolve.Issue{{Kind: "logseq-write", Context: "logseq", Detail: msg.err.Error()}}
			m.viewport.SetContent(Highlight(msg.content, m.th) + "\n\n" + issuesText(m.issues, m.st))
			m.viewport.GotoTop()
			return m, nil
		}
		m.state = stateSaving
		m.status = "reloading…"
		return m, reloadCmd(m.reload)
	case reloadedMsg:
		if msg.err != nil {
			m.state = stateList
			m.status = "reload failed: " + msg.err.Error()
			return m, nil
		}
		m.data = msg.data
		m.table.SetRows(rowsFromTasks(msg.data.Tasks))
		m.state = stateList
		m.status = "saved."
		m.editedContent = ""
		m.issues = nil
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	if m.state == stateList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	if m.state == stateView || m.state == stateInvalid {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func rowsFromTasks(tasks []model.Task) []table.Row {
	rows := make([]table.Row, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, formatTaskRow(t))
	}
	return rows
}

func issuesText(issues []resolve.Issue, st styles) string {
	if len(issues) == 0 {
		return st.ok.Render("valid.")
	}
	var sb strings.Builder
	sb.WriteString(st.err.Render(fmt.Sprintf("%d problem(s) — save blocked:", len(issues))))
	sb.WriteByte('\n')
	for _, is := range issues {
		fmt.Fprintf(&sb, "  [%s] %s: %s\n", is.Kind, is.Context, is.Detail)
	}
	sb.WriteString(st.dim.Render("e: re-edit  t: fix time-map  esc: discard"))
	return sb.String()
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.state {
	case stateList:
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			t, id, ok := m.taskAtCursor()
			if !ok {
				return m, nil
			}
			m.selectedID = id
			m.selectedTask = t
			m.state = stateView
			content := Highlight(m.rawContent(id), m.th) + footerFor(id, &m.data)
			m.viewport.SetContent(content)
			m.viewport.GotoTop()
			return m, nil
		case "e":
			_, id, ok := m.taskAtCursor()
			if !ok {
				return m, nil
			}
			m.selectedID = id
			path, cmd := editorCmd(id, m.rawContent(id))
			m.pendingTmp = path
			return m, cmd
		default:
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	case stateView:
		switch key {
		case "esc", "q":
			m.state = stateList
			return m, nil
		case "e":
			path, cmd := editorCmd(m.selectedID, m.rawContent(m.selectedID))
			m.pendingTmp = path
			return m, cmd
		case "ctrl+c":
			return m, tea.Quit
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	case stateInvalid:
		switch key {
		case "esc":
			m.state = stateList
			m.editedContent = ""
			m.issues = nil
			return m, nil
		case "e":
			path, cmd := editorCmd(m.selectedID, m.editedContent)
			m.pendingTmp = path
			return m, cmd
		case "t":
			slugs := TimeMapSlugs(&m.data)
			m.fixOptions = append([]string{"(default — clear time-map::)"}, slugs...)
			m.fixValues = append([]string{""}, slugs...)
			m.fixCursor = 0
			m.fixForID = m.selectedID
			m.state = stateFixMap
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	case stateFixMap:
		switch key {
		case "esc":
			m.state = stateInvalid
			return m, nil
		case "up", "k":
			if m.fixCursor > 0 {
				m.fixCursor--
			}
			return m, nil
		case "down", "j":
			if m.fixCursor < len(m.fixOptions)-1 {
				m.fixCursor++
			}
			return m, nil
		case "enter":
			val := m.fixValues[m.fixCursor]
			patched := PatchTimeMap(m.editedContent, val, val == "")
			m.editedContent = patched
			_, issues, ok := ValidateEdited(&m.data, m.fixForID, patched)
			if ok {
				m.state = stateSaving
				m.selectedID = m.fixForID
				m.status = "saving to Logseq…"
				return m, saveCmd(m.data, m.fixForID, patched)
			}
			m.state = stateInvalid
			m.selectedID = m.fixForID
			m.issues = issues
			m.viewport.SetContent(Highlight(patched, m.th) + "\n\n" + issuesText(issues, m.st))
			m.viewport.GotoTop()
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	case stateSaving:
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case stateList:
		help := m.st.dim.Render("enter: view  e: edit in $EDITOR  q: quit   tasks: " + fmt.Sprint(len(m.data.Tasks)))
		s := m.st.title.Render("Logseq Tasks") + "\n" + m.table.View() + "\n" + help
		if m.status != "" {
			s += "\n" + m.st.dim.Render(m.status)
		}
		if len(m.data.Issues) > 0 {
			s += "\n" + m.st.dim.Render(fmt.Sprintf("graph issues: %d (see lodo check)", len(m.data.Issues)))
		}
		return s
	case stateView:
		help := m.st.dim.Render("esc: back  e: edit  q: quit")
		return m.st.title.Render("Task") + "\n" + m.viewport.View() + "\n" + help
	case stateInvalid:
		help := m.st.dim.Render("e: re-edit  t: fix time-map  esc: discard  q: quit")
		return m.st.err.Render("Validation blocked save") + "\n" + m.viewport.View() + "\n" + help
	case stateFixMap:
		var sb strings.Builder
		sb.WriteString(m.st.title.Render("Select time-map") + "\n")
		for i, opt := range m.fixOptions {
			cursor := "  "
			if i == m.fixCursor {
				cursor = m.st.select_.Render("> ")
				sb.WriteString(cursor + m.st.select_.Render(opt) + "\n")
				continue
			}
			sb.WriteString(cursor + opt + "\n")
		}
		sb.WriteString(m.st.dim.Render("enter: apply  esc: back"))
		return sb.String()
	case stateSaving:
		return m.st.dim.Render(m.status)
	default:
		return ""
	}
}
