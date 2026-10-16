// Multi-calendar busy input (calendar.md): one write calendar plus shared
// read calendars as busy-time union. The write calendar stays the single
// write target; read calendars are Lodo-policy read-only, never written,
// even when the Google ACL would allow it.
package calendar

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/norm"
)

// Read-calendar statuses for the audit table.
const (
	// SourceRead means the calendar listed cleanly.
	SourceRead = "read"
	// SourceSkipped403 means the share answered 403 (revoked, never
	// accepted, or free/busy-only share).
	SourceSkipped403 = "skipped:403"
	// SourceSkipped404 means the share answered 404 (removed calendar).
	SourceSkipped404 = "skipped:404"
)

// SourceStatus is one read calendar's audit row: how the read went and how
// many events the API returned.
type SourceStatus struct {
	CalendarID string
	Status     string
	Events     int
}

// BusyReport is one multi-calendar read's audit: the write calendar plus
// per-read-calendar status, human warnings for the CLI, and the shared-only
// busy union refresh uses for overlap reporting.
type BusyReport struct {
	WriteCalendar string
	WriteEvents   int
	Sources       []SourceStatus
	Warnings      []string
	// Shared is the read-calendars-only busy union, over the same grid as
	// the returned mask. Empty (all free) when no read calendars read.
	Shared grid.Mask
}

// ReadOptions selects which read-calendar events contribute busy time.
// The zero value keeps every non-free event, including all-day ones.
// The write calendar is never affected.
type ReadOptions struct {
	// IgnoreAllDay drops every all-day event on a read calendar.
	IgnoreAllDay bool
	// IgnoreAllDayMatching drops an all-day read-calendar event whose
	// summary contains one of these phrases. Timed events are never
	// matched. Unused when IgnoreAllDay is set. Phrases are normalized
	// (trim, lowercase, drop empties and duplicates) at construction.
	IgnoreAllDayMatching []string
}

// MultiClient reads the write calendar plus each read calendar over the
// scheduling window, OR-ing their busy masks. Reads run sequentially in
// config order so warning output is deterministic. One read calendar
// failing 403/404 does not stop the remaining reads; auth and network
// failures still fail the run.
type MultiClient struct {
	write                *Client
	reads                []*Client
	writeID              string
	readIDs              []string
	ignoreAllDay         bool
	ignoreAllDayMatching []string
}

// NewMultiClient starts one multi-calendar reader: writeID is the single
// write calendar, readIDs are shared busy sources in config order. base ""
// points at Google; tests point at a fake. hc nil means the default
// transport with DefaultTimeout. Read IDs are normalized like config
// (trim, drop empties, drop the write ID, dedupe) so a duplicate write ID
// in the read list never double-reads. opt applies only to read calendars.
func NewMultiClient(hc *http.Client, writeID string, readIDs []string, base string, opt ReadOptions) (*MultiClient, error) {
	writeID = strings.TrimSpace(writeID)
	if writeID == "" {
		return nil, fmt.Errorf("google calendar client: no calendar id (config.yaml's calendar.id)")
	}
	clean := norm.ReadCalendars(writeID, readIDs)
	write, err := NewClient(hc, writeID, base)
	if err != nil {
		return nil, err
	}
	m := &MultiClient{
		write: write, writeID: writeID, readIDs: clean,
		ignoreAllDay:         opt.IgnoreAllDay,
		ignoreAllDayMatching: norm.Phrases(opt.IgnoreAllDayMatching),
	}
	for _, id := range clean {
		c, err := NewClient(hc, id, base)
		if err != nil {
			return nil, err
		}
		m.reads = append(m.reads, c)
	}
	return m, nil
}

// WriteID returns the single write calendar's ID.
func (m *MultiClient) WriteID() string { return m.writeID }

// ReadIDs returns the normalized read calendar IDs in order.
func (m *MultiClient) ReadIDs() []string { return append([]string(nil), m.readIDs...) }

// List returns the write calendar's events overlapping [from, to): the sole
// owner of lodo=true semantics. Read calendars never flow into
// reconciliation.
func (m *MultiClient) List(ctx context.Context, from, to time.Time) ([]Event, error) {
	return m.write.List(ctx, from, to)
}

// Insert creates one Managed event on the write calendar only.
func (m *MultiClient) Insert(ctx context.Context, e Event) (Event, error) {
	return m.write.Insert(ctx, e)
}

// Patch updates one Managed event on the write calendar only.
func (m *MultiClient) Patch(ctx context.Context, id string, e Event) (Event, error) {
	return m.write.Patch(ctx, id, e)
}

// Delete removes one Managed event on the write calendar only.
func (m *MultiClient) Delete(ctx context.Context, id string) error {
	return m.write.Delete(ctx, id)
}

// Busy lists the write calendar plus each read calendar over the grid's
// window, classifies with the same rules on every calendar, and ORs the
// masks. Managed-event detection is write-calendar-only: a lodo=true event
// on a read calendar is ordinary busy time plus a warning, never adopted,
// never moved. Per-calendar 403/404 warns and continues; anything else
// fails the run.
func (m *MultiClient) Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []Event, BusyReport, error) {
	report := BusyReport{WriteCalendar: m.writeID, Shared: grid.NewMask(g.Len())}
	union := grid.NewMask(g.Len())

	writeEvents, managed, err := m.write.listBusy(ctx, g, now)
	if err != nil {
		return nil, nil, BusyReport{}, err
	}
	report.WriteEvents = len(writeEvents)
	writeBusy, err := busyMask(g, writeEvents, now)
	if err != nil {
		return nil, nil, BusyReport{}, err
	}
	union.OrInto(writeBusy)

	for i, rc := range m.reads {
		id := m.readIDs[i]
		events, err := rc.List(ctx, now, g.WindowEnd())
		if err != nil {
			if code, ok := StatusCodeOf(err); ok {
				switch code {
				case http.StatusForbidden:
					report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceSkipped403, Events: 0})
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("read calendar %q skipped (403): access denied (revoked, never accepted, or free/busy-only share; free/busy-only shares are not yet supported)", id))
					continue
				case http.StatusNotFound:
					report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceSkipped404, Events: 0})
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("read calendar %q skipped (404): not found (removed calendar)", id))
					continue
				}
			}
			return nil, nil, BusyReport{}, err
		}
		report.Sources = append(report.Sources, SourceStatus{CalendarID: id, Status: SourceRead, Events: len(events)})
		for _, e := range events {
			if e.Managed() {
				// Write-calendar-only detection: on a read calendar a
				// lodo=true event is ordinary busy time (never adopted),
				// plus a warning. Cancelled/transparent/declined are
				// ignored here: a copied Managed event is busy, the way
				// decision 7 states it.
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("read calendar %q: event %q carries lodo=true, treated as ordinary busy time (never adopted, never moved)", id, e.ID))
			} else if Classify(e) == KindFree {
				continue
			}
			if m.skipReadAllDay(e) {
				continue
			}
			start, end, err := projectSpan(e, g, now)
			if err != nil {
				return nil, nil, BusyReport{}, err
			}
			if start.IsZero() {
				continue
			}
			slot := g.Overlapping(start, end)
			union.OrInto(slot)
			report.Shared.OrInto(slot)
		}
	}
	return union, managed, report, nil
}

// skipReadAllDay reports whether a read-calendar all-day event is free by
// config. Timed events are never skipped. A configured skip is silent: the
// event still counts in the audit total and does not enter the mask.
func (m *MultiClient) skipReadAllDay(e Event) bool {
	if !e.AllDay() {
		return false
	}
	if m.ignoreAllDay {
		return true
	}
	summary := strings.ToLower(e.Summary)
	for _, phrase := range m.ignoreAllDayMatching {
		if strings.Contains(summary, phrase) {
			return true
		}
	}
	return false
}

// listBusy is Client.Busy's list-plus-classify core, returning the raw
// events, the managed subset, and projecting busy onto a fresh mask. It is
// shared so single- and multi-calendar reads classify identically.
func (c *Client) listBusy(ctx context.Context, g *grid.Grid, now time.Time) (events []Event, managed []Event, err error) {
	events, err = c.List(ctx, now, g.WindowEnd())
	if err != nil {
		return nil, nil, err
	}
	return events, managedEvents(events), nil
}

// managedEvents filters the write calendar's lodo=true events.
func managedEvents(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Managed() {
			out = append(out, e)
		}
	}
	return out
}

// projectSpan clips an event's span to the run window for mask projection:
// zero start means the window never touches the rest of it (skip). Errors
// are data corruption and fail the run, the way a revoked refresh does.
func projectSpan(e Event, g *grid.Grid, now time.Time) (start, end time.Time, err error) {
	start, end, err = e.Span(g.Location())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if start.Before(now) {
		start = now
	}
	if end.After(g.WindowEnd()) {
		end = g.WindowEnd()
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, nil
	}
	return start, end, nil
}
