// Package calendar is the Google Calendar integration (calendar.md): list
// the events a window overlaps, classify them, project busy time onto the
// run's grid, and write Managed events back. Busy arrives to the planner as
// an explicit mask (ADR 0006). All requests are plain net/http plus
// encoding/json, deliberately not google.golang.org/api: the REST surface
// v0.1 needs is list, get, insert, patch and delete.
package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/grid"
)

// GoogleBase is the Calendar API v3 root the real client talks to.
const GoogleBase = "https://www.googleapis.com/calendar/v3"

const (
	// DefaultTimeout bounds one API call, like the Logseq client's: one
	// that sits on a wall for half a minute has stopped answering.
	DefaultTimeout = 30 * time.Second
	// readLimit caps one response: event lists are the big payloads and
	// anything bigger than this is not a calendar a user wants read.
	readLimit = 64 << 20
)

// The event fields the busy rules read.
const (
	statusCancelled   = "cancelled"
	statusTransparent = "transparent"
	statusDeclined    = "declined"
)

// Client reads one Calendar over the API v3 REST surface. Its transport is
// supplied by the caller: production wraps the OAuth token source so
// refreshes happen transparently (the credentials' refresh token was
// stored by `lodo auth google`); a failed refresh surfaces as an error
// when a call is made, never as an empty mask.
type Client struct {
	hc   *http.Client
	base *url.URL
	id   string
}

// NewClient starts one reader for the calendar id names (for example the
// config's calendar.id, default primary). base "" points it at Google
// (GoogleBase); tests point it at an httptest fake. hc nil means the
// default transport with DefaultTimeout.
func NewClient(hc *http.Client, id, base string) (*Client, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("google calendar client: no calendar id (config.yaml's calendar.id)")
	}
	if base == "" {
		base = GoogleBase
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("google calendar client: base %q: must be an absolute http(s) URL", base)
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{hc: hc, base: u, id: id}, nil
}

// Event is one Calendar API v3 event, in the parts M4 recognises in the
// wild (calendar.md's shape section read, not written). Identifying fields
// are absent-ish on the wire without changing decode: every event carries
// its id, and every timed event its start and end.
type Event struct {
	ID                 string     `json:"id"`
	Summary            string     `json:"summary,omitempty"`
	Status             string     `json:"status,omitempty"`
	Transparency       string     `json:"transparency,omitempty"`
	Start              EventTime  `json:"start,omitempty"`
	End                EventTime  `json:"end,omitempty"`
	Recurrence         []string   `json:"recurrence,omitempty"`
	Attendees          []Attendee `json:"attendees,omitempty"`
	ExtendedProperties Extended   `json:"extendedProperties,omitempty"`
	Reminders          *Reminders `json:"reminders,omitempty"`
}

// Reminders selects the calendar's defaults on write; v0.1 never sets
// per-event reminders (calendar.md: reminders use the calendar's defaults).
type Reminders struct {
	UseDefault bool `json:"useDefault"`
}

// EventTime is the start or end of an event: either a timed instant
// (dateTime, RFC 3339 with its own offset) or a whole day (date,
// yyyy-mm-dd; the day an all-day event ends is the one after its start).
// TimeZone carries the Scheduling timezone on write; reads ignore it and
// use dateTime's own offset.
type EventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

// Attendee is one attendee and their response. The user's own attendance
// (self=true) decides whether the event blocks their time.
type Attendee struct {
	Self           bool   `json:"self,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
}

// Extended is the event's property bag; the private "lodo" flag marks a
// Managed Event, so ownership survives Lodo losing its local state.
type Extended struct {
	Private map[string]string `json:"private,omitempty"`
}

// Kind is what an event means to busy time (calendar.md).
type Kind string

const (
	// KindManaged is an event Lodo itself created: left out of busy time
	// and folded back in by reconciliation.
	KindManaged Kind = "managed"
	// KindBusy blocks the time it touches.
	KindBusy Kind = "busy"
	// KindFree blocks nothing: cancelled, declined by the user, or marked
	// transparent.
	KindFree Kind = "free"
)

// Classify applies calendar.md's busy rules as a pure function of the
// event: Managed iff it carries lodo=true in its private properties; free
// when cancelled, declined by the user, or transparent; busy otherwise.
// All-day events classify like any other — busy — and Span gives them
// their whole local day.
func Classify(e Event) Kind {
	if e.Managed() {
		return KindManaged
	}
	if e.Status == statusCancelled || e.userDeclined() || e.Transparency == statusTransparent {
		return KindFree
	}
	return KindBusy
}

// Managed reports whether the event is one Lodo itself created — the
// ownership test calendar.md records on the event itself. Everything else
// is an External Event: busy time, never moved.
func (e Event) Managed() bool {
	return e.ExtendedProperties.Private["lodo"] == "true"
}

// AllDay reports Google's all-day shape: a start date and no dateTime.
// A timed event with an unparseable dateTime is not all-day; Span falls
// back to the date for projection, and the write calendar still counts it.
func (e Event) AllDay() bool {
	return strings.TrimSpace(e.Start.DateTime) == "" && strings.TrimSpace(e.Start.Date) != ""
}

// userDeclined reports whether the user, as the attendee marked self=true,
// declined. Other attendees' responses are not the user's.
func (e Event) userDeclined() bool {
	for _, a := range e.Attendees {
		if a.Self && a.ResponseStatus == statusDeclined {
			return true
		}
	}
	return false
}

// Span returns [start, end) as instants. A timed event uses the RFC 3339
// offsets it carries; a missing or unparseable dateTime falls back to the
// event's date, read in loc (the Scheduling timezone), so an all-day event
// is busy for the whole local day — Google ends such an event on the day
// after. An end that does not follow its start is data corruption and
// erroring refuses it into the open, the way a revoked refresh does.
func (e Event) Span(loc *time.Location) (start, end time.Time, err error) {
	start, err = instant(e.Start, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("event %s: start: %w", e.ID, err)
	}
	end, err = instant(e.End, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("event %s: end: %w", e.ID, err)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("event %s: ends %v, not after it starts %v", e.ID, end, start)
	}
	return start, end, nil
}

// instant parses one EventTime: its dateTime first (RFC 3339), its date
// second (the midnight of that day in loc), and an error when there is
// neither.
func instant(et EventTime, loc *time.Location) (time.Time, error) {
	if s := strings.TrimSpace(et.DateTime); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, nil
		}
	}
	if s := strings.TrimSpace(et.Date); s != "" {
		if d, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return d, nil
		}
	}
	return time.Time{}, fmt.Errorf("neither dateTime (%q) nor date (%q) parses", et.DateTime, et.Date)
}

// List returns the expanded events that overlap [from, to). singleEvents
// expands recurrences into the instances that matter, and makes the
// server's orderBy=startTime legal: pages read forward by start. Ties are
// the server's business alone, for a union mask never depends on them.
// Pages follow nextPageToken until Google is out.
func (c *Client) List(ctx context.Context, from, to time.Time) ([]Event, error) {
	q := url.Values{}
	q.Set("timeMin", from.Format(time.RFC3339))
	q.Set("timeMax", to.Format(time.RFC3339))
	q.Set("singleEvents", "true")
	q.Set("orderBy", "startTime")
	q.Set("maxResults", "2500")

	var all []Event
	for {
		var page struct {
			Items         []Event `json:"items"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := c.get(ctx, []string{"calendars", c.id, "events"}, q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextPageToken == "" {
			return all, nil
		}
		q.Set("pageToken", page.NextPageToken)
	}
}

// Get fetches one event by id. Reconciliation works from List (a missing
// Managed event in the window is a user deletion); Get is for tests and
// curious callers. A missing event is the client's ordinary error.
func (c *Client) Get(ctx context.Context, id string) (Event, error) {
	var e Event
	if err := c.get(ctx, []string{"calendars", c.id, "events", id}, nil, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Insert creates one Managed event (events.insert) with its deterministic
// id, so re-inserting is idempotent and cannot duplicate. The payload
// carries calendar.md's shape: title is the Task's own text, times in the
// Scheduling timezone, private lodo/task/block ownership, and the
// calendar's default reminders.
func (c *Client) Insert(ctx context.Context, e Event) (Event, error) {
	var out Event
	if err := c.write(ctx, http.MethodPost, []string{"calendars", c.id, "events"}, e, &out); err != nil {
		return Event{}, err
	}
	return out, nil
}

// Patch updates one Managed event's times (and refreshes its summary to
// the Task's current text; Pinned Blocks adopt the text without gaining
// markers). Times only shape the move; the summary refresh is what keeps a
// renamed Task's events titled.
func (c *Client) Patch(ctx context.Context, id string, e Event) (Event, error) {
	var out Event
	if err := c.write(ctx, http.MethodPatch, []string{"calendars", c.id, "events", id}, e, &out); err != nil {
		return Event{}, err
	}
	return out, nil
}

// Delete removes one Managed event (events.delete): the Task gone from the
// graph, or a Block no longer in the plan.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.write(ctx, http.MethodDelete, []string{"calendars", c.id, "events", id}, nil, nil)
}

// Busy is the run's calendar read: list the events that overlap the
// grid's window, classify them, and project the busy ones onto g with the
// conservative overlap the planner already uses — a slot a busy event
// touches is busy whole. Events starting before now or running past the
// window's end are clipped before projecting: an event crossing a window
// boundary clips, it does not become busy for the whole window. Managed
// events carry no busy time in M4 — no dispositions exist yet — and are
// returned instead, so forecast can name the hole schedule closes.
//
// The report carries the write calendar and its event count; single-calendar
// reads have no sources, no warnings, and an empty shared mask.
func (c *Client) Busy(ctx context.Context, g *grid.Grid, now time.Time) (grid.Mask, []Event, BusyReport, error) {
	events, err := c.List(ctx, now, g.WindowEnd())
	if err != nil {
		return nil, nil, BusyReport{}, err
	}

	mask, err := busyMask(g, events, now)
	if err != nil {
		return nil, nil, BusyReport{}, err
	}
	report := BusyReport{
		WriteCalendar: c.id,
		WriteEvents:   len(events),
		Shared:        grid.NewMask(g.Len()),
	}
	return mask, managedEvents(events), report, nil
}

// busyMask projects the busy subset of events onto g: managed and free
// events contribute nothing. Clipping to [now, window end] happens in
// projectSpan; zero start means the window never touches the event.
func busyMask(g *grid.Grid, events []Event, now time.Time) (grid.Mask, error) {
	mask := grid.NewMask(g.Len())
	for _, e := range events {
		if e.Managed() || Classify(e) == KindFree {
			continue
		}
		start, end, err := projectSpan(e, g, now)
		if err != nil {
			return nil, err
		}
		if start.IsZero() {
			continue // the window never touches the rest of it
		}
		mask.OrInto(g.Overlapping(start, end))
	}
	return mask, nil
}

// get issues one GET against base+path+query and decodes out (which may be
// JSON null). A failure reads Google's error body, {"error": …}.
func (c *Client) get(ctx context.Context, path []string, q url.Values, out any) error {
	u := c.base.JoinPath(path...)
	if q != nil {
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("google calendar api: %w", err)
	}
	return c.do(req, strings.Join(path, "/"), out, http.StatusOK)
}

// write issues one mutating call (POST/PATCH/DELETE) and decodes out when
// non-nil. DELETE expects 204 or 200 with an empty body; a missing event on
// delete is surfaced as the API's 404, which the caller treats as already
// gone. Every failure reads Google's error body, {"error": …}.
func (c *Client) write(ctx context.Context, method string, path []string, in any, out any) error {
	u := c.base.JoinPath(path...)

	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("google calendar api: %s: %w", strings.Join(path, "/"), err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return fmt.Errorf("google calendar api: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, strings.Join(path, "/"), out, http.StatusOK, http.StatusCreated, http.StatusNoContent)
}

// do executes one API request, checks its status, and decodes out when the
// body is non-empty. Failures carry Google's {"error":{"message"}} or a body
// snippet. A response body's close error is never actionable.
func (c *Client) do(req *http.Request, path string, out any, okStatuses ...int) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("google calendar api: %s: %w", path, errorCause(err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	if err != nil {
		return fmt.Errorf("google calendar api: %s: %w", path, err)
	}
	ok := false
	for _, s := range okStatuses {
		if resp.StatusCode == s {
			ok = true
			break
		}
	}
	if !ok {
		msg := decodeError(data)
		if msg == "" {
			msg = snippet(data)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg, Path: path}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("google calendar api: %s: decode: %w (body: %s)", path, err, snippet(data))
	}
	return nil
}

// APIError is one Google Calendar API failure with its HTTP status, so the
// multi-calendar read can tell a per-calendar 403/404 (warn-and-continue)
// from an auth or network failure (fail the run). Message is Google's own
// {"error":{"message"}} or a body snippet.
type APIError struct {
	StatusCode int
	Message    string
	Path       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("google calendar api: %s: HTTP %d: %s", e.Path, e.StatusCode, e.Message)
}

// StatusCodeOf unwraps an API failure's HTTP status. False when the error is
// not an API failure (network, auth refresh, decode).
func StatusCodeOf(err error) (int, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, true
	}
	return 0, false
}

// decodeError reads Google's error body: {"error": {"message": "…"}}.
func decodeError(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return ""
}

// errorCause shortens the transport noise: a *url.Error wraps the real
// cause (dial refused, timeout), so it is unwrapped for the message.
func errorCause(err error) error {
	var uErr *url.Error
	if errors.As(err, &uErr) {
		return uErr.Err
	}
	return err
}

// snippet renders a short body prefix for error messages.
func snippet(data []byte) string {
	const at = 300
	s := string(data)
	if len(s) > at {
		s = s[:at] + "…"
	}
	return strings.TrimSpace(s)
}
