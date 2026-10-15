// The Google composition: how the CLI hands `run` its busy input, and how
// the credentials' google section becomes a Calendar-reading client. The
// files stay outside this package's concerns; gauth and config own them.
package cli

import (
	"context"

	"golang.org/x/oauth2"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/gauth"
	"github.com/aries1980/lodo/internal/run"
)

// googleCalendarBase is the Calendar API v3 root. Like googleEndpoint in
// auth.go, it is a var so the white-box tests can point both at one
// httptest fake; production gets Google's own.
var googleCalendarBase = calendar.GoogleBase

// calendarBusy builds the read side forecast consumes: the Calendar API
// reader OAuthed through the credentials' google section, refreshing
// transparently via the token source `lodo auth google` set up. Errors are
// operational and exit 1 by construction of the caller — a missing refresh
// token is the auth hint, a revoked one or an unreachable calendar the
// operational cause; never a silently empty mask.
func calendarBusy(ctx context.Context, creds config.Credentials, cfg config.Config) (run.BusySource, error) {
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
	return calendar.NewClient(hc, cfg.Calendar.ID, googleCalendarBase)
}
