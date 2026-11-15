package cli

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// nowDefault reads LODO_NOW, falling back to empty (the wall clock).
func nowDefault() string {
	return strings.TrimSpace(os.Getenv("LODO_NOW"))
}

// resolveNow pins the scheduling clock from --now (whose default is
// LODO_NOW). The hook runs before any command body, so a malformed value
// fails before config, graph, or calendar I/O.
func (o *options) resolveNow() error {
	if o.nowValue == "" {
		return nil
	}
	at, err := time.Parse(time.RFC3339, o.nowValue)
	if err != nil {
		return fmt.Errorf("--now: %w", err)
	}
	o.now = func() time.Time { return at }
	return nil
}

// currentTime is the run's scheduling instant: the pinned clock when one is
// set, otherwise the wall clock at the moment prepare finishes its I/O.
func (o *options) currentTime() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}
