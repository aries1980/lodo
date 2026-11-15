package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/run"
)

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
func printBusyText(w io.Writer, r calendar.BusyReport, st styler) {
	if r.WriteCalendar == "" {
		return
	}
	if line := busySourcesLine(r); line != "" {
		_, _ = fmt.Fprintln(w, st.dim(line))
	}
	for _, warn := range r.Warnings {
		_, _ = fmt.Fprintln(w, st.warning("warning: "+warn))
	}
}

// printConflictsText warns about Managed Blocks that now overlap shared busy
// time, naming each Block. Refresh changes nothing in response. Tasks may be
// nil (refresh never resolves the graph), in which case Blocks are named by
// their Task ID.
func printConflictsText(w io.Writer, tasks []model.Task, conflicts []run.Conflict, st styler) {
	byText := textByTasks(tasks)
	for _, c := range conflicts {
		label := c.EventID
		if c.TaskID != "" {
			name := byText[c.TaskID]
			if name == "" {
				name = c.TaskID
			}
			label = fmt.Sprintf("%s block %d (%s)", name, c.BlockIndex, c.EventID)
		}
		_, _ = fmt.Fprintln(w, st.warning("warning: Managed Block "+label+" now overlaps shared busy time (no moves, no stand-downs; re-placement happens only in schedule)"))
	}
}

// logBusyWarnings mirrors busy warnings as JSON WARN records for CI/cron.
func logBusyWarnings(opts *options, command string, r calendar.BusyReport) {
	for _, warn := range r.Warnings {
		logRunWarn(opts, command, "calendar.warning", warn, map[string]any{
			"write_calendar": r.WriteCalendar,
		})
	}
}
