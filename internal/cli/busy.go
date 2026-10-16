package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/run"
)

// busySourceJSON is one calendar's audit row for --json.
type busySourceJSON struct {
	CalendarID string `json:"calendar_id"`
	Status     string `json:"status"`
	Events     int    `json:"events"`
}

// busyJSON is the machine-readable busy audit: the write calendar plus
// per-read-calendar status and the warnings the text prints as warning lines.
type busyJSON struct {
	WriteCalendar string           `json:"write_calendar"`
	WriteEvents   int              `json:"write_events"`
	Sources       []busySourceJSON `json:"sources,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
}

// busyView converts a calendar read report to its JSON view. Sources stay in
// config order; warnings stay in read order.
func busyView(r calendar.BusyReport) busyJSON {
	view := busyJSON{
		WriteCalendar: r.WriteCalendar,
		WriteEvents:   r.WriteEvents,
		Warnings:      append([]string(nil), r.Warnings...),
	}
	for _, s := range r.Sources {
		view.Sources = append(view.Sources, busySourceJSON{
			CalendarID: s.CalendarID, Status: s.Status, Events: s.Events,
		})
	}
	if len(view.Warnings) == 0 {
		view.Warnings = nil
	}
	return view
}

// busySourcesLine renders the audit line every calendar-reading command
// prints: the write calendar with its event count, then each read calendar
// with its count or its skip status. Empty when no calendar was consulted
// (check without credentials).
func busySourcesLine(r calendar.BusyReport) string {
	if r.WriteCalendar == "" {
		return ""
	}
	parts := []string{fmt.Sprintf("%s (%d events)", r.WriteCalendar, r.WriteEvents)}
	for _, s := range r.Sources {
		if s.Status == calendar.SourceRead {
			parts = append(parts, fmt.Sprintf("%s (%d events)", s.CalendarID, s.Events))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", s.CalendarID, s.Status))
	}
	return "busy sources: " + strings.Join(parts, ", ")
}

// printBusyText prints the busy sources line plus one warning line per
// skipped calendar or lodo-on-read event. No output when no calendar was
// consulted.
func printBusyText(w io.Writer, r calendar.BusyReport) {
	if r.WriteCalendar == "" {
		return
	}
	if line := busySourcesLine(r); line != "" {
		_, _ = fmt.Fprintln(w, line)
	}
	for _, warn := range r.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", warn)
	}
}

// conflictJSON is one shared-calendar overlap for --json.
type conflictJSON struct {
	TaskID     string `json:"task_id,omitempty"`
	BlockIndex int    `json:"block_index"`
	EventID    string `json:"event_id"`
	Summary    string `json:"summary,omitempty"`
}

// conflictsView converts refresh conflicts to their JSON view, ordered by
// event ID.
func conflictsView(conflicts []run.Conflict) []conflictJSON {
	out := make([]conflictJSON, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, conflictJSON{
			TaskID: c.TaskID, BlockIndex: c.BlockIndex, EventID: c.EventID, Summary: c.Summary,
		})
	}
	return out
}

// printConflictsText warns about Managed Blocks that now overlap shared busy
// time, naming each Block. Refresh changes nothing in response. Tasks may be
// nil (refresh never resolves the graph), in which case Blocks are named by
// their Task ID.
func printConflictsText(w io.Writer, tasks []model.Task, conflicts []run.Conflict) {
	byText := run.TextMap(tasks)
	for _, c := range conflicts {
		label := c.EventID
		if c.TaskID != "" {
			name := byText[c.TaskID]
			if name == "" {
				name = c.TaskID
			}
			label = fmt.Sprintf("%s block %d (%s)", name, c.BlockIndex, c.EventID)
		}
		_, _ = fmt.Fprintf(w, "warning: Managed Block %s now overlaps shared busy time (no moves, no stand-downs; re-placement happens only in schedule)\n", label)
	}
}
