package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/run"
	"github.com/aries1980/lodo/internal/store"
	"github.com/aries1980/lodo/internal/theme"
)

// calendarMode says how a command treats the Google Calendar client.
type calendarMode uint8

const (
	// calendarNone builds no calendar client at all (the TUI).
	calendarNone calendarMode = iota
	// calendarRequired aborts the run on any calendar failure: forecast,
	// schedule and refresh must never report against a silently free calendar.
	calendarRequired
	// calendarOptional degrades to the stored state when no Google grant
	// exists yet; any other calendar failure still aborts. See check and
	// explain.
	calendarOptional
)

// needs declares the outside world one command touches. The rows live in
// commandNeeds, so that table is the single answer to "what does this command
// read or write?". Nothing reads a needs value directly: prepare is the only
// consumer.
type needs struct {
	// graph builds the Logseq clients and resolves their tokens, then probes
	// every graph before any calendar or store I/O (ADR 0002).
	graph bool
	// timeMaps ensures the configured Time Maps page exists, on an explicit
	// yes only (ADR 0015).
	timeMaps bool
	// store opens the SQLite system of record for the run (ADR 0009).
	store bool
	// calendar is how the command tolerates the Google Calendar.
	calendar calendarMode
}

// commandNeeds is the one table of what each command touches.
var commandNeeds = map[string]needs{
	"check":    {graph: true, timeMaps: true, store: true, calendar: calendarOptional},
	"forecast": {graph: true, timeMaps: true, calendar: calendarRequired},
	"schedule": {graph: true, timeMaps: true, store: true, calendar: calendarRequired},
	"explain":  {graph: true, timeMaps: true, store: true, calendar: calendarOptional},
	"refresh":  {store: true, calendar: calendarRequired},
	"tui":      {graph: true},
}

// command is one handler's opened world: the config and credentials it was
// prepared with, the graph clients, the calendar, and the store. cal is nil
// only when the command declared calendarOptional and no Google grant exists,
// so a command that declared calendarRequired can hand it straight to run.
type command struct {
	opts    *options
	name    string
	start   time.Time
	cfg     config.Config
	sources []run.Source
	cal     *calendar.MultiClient
	store   *store.Store
	style   styler
	theme   theme.Theme
	now     time.Time
}

// prepare opens everything the named command declared in commandNeeds, in one
// order: config and credentials, the graph clients and their probe, the Time
// Maps page, the calendar, then the store. The store opens last so a run that
// fails on credentials leaves no database behind; now is captured after the
// network work, from the pinned clock (--now/LODO_NOW) when one is set.
func prepare(ctx context.Context, opts *options, ov configOverrides, yes bool, name string) (*command, error) {
	n, ok := commandNeeds[name]
	if !ok {
		return nil, fmt.Errorf("command %q has no declared needs", name)
	}
	if n.timeMaps && !n.graph {
		return nil, errors.New("timeMaps needs the graph clients to write the page with")
	}

	cfg, creds, err := loadConfig(opts, ov)
	if err != nil {
		return nil, err
	}
	c := &command{
		opts:  opts,
		name:  name,
		start: logRunStart(opts, name),
		cfg:   cfg,
	}
	if n.graph {
		if c.sources, err = buildSources(cfg, creds); err != nil {
			return nil, err
		}
	}

	th, warn := resolveTheme(cfg, opts.config)
	if warn != "" {
		_, _ = fmt.Fprintln(errWriter(opts), warn)
	}
	c.theme = th
	c.style = newStyler(opts.out, opts.disableColors, th)

	if n.graph {
		if err := checkLogseqAvailable(ctx, c.sources, makeLogseqProbeNotify(opts, name, th)); err != nil {
			return nil, err
		}
	}
	if n.timeMaps {
		if _, err := ensureTimeMapsPage(ctx, c.sources[0].Client, cfg.Logseq.PrimaryTimeMapsPage(), yes,
			errWriter(opts), newStyler(errWriter(opts), opts.disableColors, th)); err != nil {
			return nil, err
		}
	}
	if n.calendar != calendarNone {
		cal, err := calendarFull(ctx, creds, cfg)
		switch {
		case err == nil:
			c.cal = cal
		case n.calendar == calendarOptional && isMissingGoogleCredentials(err):
			// No grant yet: the command still reports the graph and the
			// stored dispositions, planned against no busy time.
		default:
			return nil, err
		}
	}
	if n.store {
		st, err := openStore()
		if err != nil {
			return nil, err
		}
		c.store = st
	}

	c.now = opts.currentTime()
	return c, nil
}

// close releases what prepare opened. Commands that asked for no store call it
// too: closing nothing is a no-op.
func (c *command) close() {
	if c.store == nil {
		return
	}
	_ = c.store.Close()
}

// finish emits the command's run.finish record.
func (c *command) finish(attrs map[string]any) {
	logRunFinish(c.opts, c.name, c.start, attrs)
}

// checkOutcome plans the graph against real busy time when a calendar is
// available and against stored dispositions alone when it is not. check and
// explain share it so the degraded path cannot drift.
func (c *command) checkOutcome(ctx context.Context) (run.CheckOutcome, error) {
	if c.cal == nil {
		return buildCheckOutcomeWithoutCalendar(ctx, c.cfg, c.sources, c.store, c.now)
	}
	return run.CheckSources(ctx, c.sources, c.cal, c.store, c.cfg, c.now)
}

// isMissingGoogleCredentials reports the one calendar failure check and
// explain degrade on: no Google grant has been stored yet. A revoked grant or
// an unreachable calendar stays operational. The wording still belongs to
// config; handoff 18 replaces this string test with a typed error.
func isMissingGoogleCredentials(err error) bool {
	return strings.Contains(err.Error(), "google credentials")
}
