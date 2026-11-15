// The Google composition: how the CLI hands `run` its busy input, and how
// the credentials' google section becomes a Calendar-reading client. The
// files stay outside this package's concerns; gauth and config own them.
package cli

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/gauth"
	"github.com/aries1980/lodo/internal/store"
)

// googleCalendarBase is the Calendar API v3 root. Like googleEndpoint in
// auth.go, it is a var so the white-box tests can point both at one
// httptest fake; production gets Google's own.
var googleCalendarBase = calendar.GoogleBase

// calendarFull builds the calendar client a command needs: the credentials'
// google section OAuthed through the token source `lodo auth google` set up,
// refreshing transparently. List and Busy shape the plan over the write and
// read calendars (ADR 0013); inserts, patches and deletes are available on the
// write calendar only.
func calendarFull(ctx context.Context, creds config.Credentials, cfg config.Config) (*calendar.MultiClient, error) {
	g, err := creds.GoogleAuth()
	if err != nil {
		return nil, err
	}
	src, err := gauth.TokenSource(ctx, googleEndpoint, g)
	if err != nil {
		return nil, err
	}
	hc := oauth2.NewClient(ctx, src)
	hc.Timeout = calendar.DefaultTimeout
	return calendar.NewMultiClient(hc, cfg.Calendar.ID, cfg.Calendar.ReadCalendars, googleCalendarBase, calendar.ReadOptions{
		IgnoreAllDay:         cfg.Calendar.IgnoreReadAllDay,
		IgnoreAllDayMatching: cfg.Calendar.IgnoreReadAllDayMatching,
	})
}

// openStore opens the system of record at its XDG path, creating it when
// absent. The store is opened per run; there is no daemon.
func openStore() (*store.Store, error) {
	path, err := config.DefaultStorePath()
	if err != nil {
		return nil, fmt.Errorf("locate the store file: %w", err)
	}
	return store.Open(path)
}
