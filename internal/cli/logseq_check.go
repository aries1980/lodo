// The Logseq fail-early gate: every loop command that reads the graph
// (check/forecast/schedule/explain) probes each source before touching the
// calendar or the store, so a stopped app or wrong api_url fails here with
// an actionable hint instead of surfacing mid-read as a bare transport
// error (ADR 0002: Logseq must be running with the API server enabled).
// Refresh never reads the graph and auth never dials it, so neither calls
// this.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/run"
)

// Probe schedule: the first Ping gets logseqProbeInitialTimeout and each
// retry doubles it, so a momentarily busy app (index re-build, GC pause)
// gets more chances while a truly dead one still fails fast. With the
// defaults (2s, 3 attempts) the timeouts are 2s → 4s → 8s and the worst
// case per source is ~14s, against the adapter's 30s single-call bound for
// real reads. Vars (not consts) so tests can shrink them against stub
// servers.
var (
	logseqProbeInitialTimeout = 2 * time.Second
	logseqProbeAttempts       = 3
)

// logseqProbeTimeoutForAttempt returns the timeout for the 0-indexed
// attempt: initial << attempt, i.e. 2s, 4s, 8s at the defaults.
// Growth is exponential by design: each retry allows twice as long,
// backing off aggressively from a struggling app.
func logseqProbeTimeoutForAttempt(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	return logseqProbeInitialTimeout << attempt
}

// logseqProbeAttempt describes one failed probe that will be retried.
// It is the payload the gate hands to its notify callback so humans and
// JSON logs report the same facts.
type logseqProbeAttempt struct {
	Source      run.Source
	Attempt     int // 1-indexed attempt that just failed
	Attempts    int // total attempts planned for this source
	Timeout     time.Duration
	NextTimeout time.Duration
	Err         error
}

// checkLogseqAvailable pings every source in order. The first source that
// stays down through all its attempts aborts the run; later graphs are not
// probed so the error names one endpoint to fix. Each attempt gets its own
// probe timeout from the logarithmic schedule, so one hung app cannot
// stall the gate past the schedule sum (times the graph count).
//
// Auth rejections fail immediately without retrying or notifying: a wrong
// token will not fix itself on a second attempt. A cancelled parent
// context also aborts without further attempts. notify, when non-nil, is
// called after each failed attempt that will be retried; tests pass nil.
func checkLogseqAvailable(ctx context.Context, sources []run.Source, notify func(logseqProbeAttempt)) error {
	for _, s := range sources {
		if s.Client == nil {
			continue
		}
		attempts := logseqProbeAttempts
		if attempts < 1 {
			attempts = 1
		}
		var lastErr error
		for attempt := 0; attempt < attempts; attempt++ {
			timeout := logseqProbeTimeoutForAttempt(attempt)
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			err := s.Client.Ping(probeCtx)
			cancel()
			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
			if isLogseqAuthFailure(err.Error()) || ctx.Err() != nil {
				break
			}
			if attempt+1 < attempts && notify != nil {
				notify(logseqProbeAttempt{
					Source:      s,
					Attempt:     attempt + 1,
					Attempts:    attempts,
					Timeout:     timeout,
					NextTimeout: logseqProbeTimeoutForAttempt(attempt + 1),
					Err:         err,
				})
			}
		}
		if lastErr != nil {
			return logseqUnavailableError(s, lastErr, attempts)
		}
	}
	return nil
}

// makeLogseqProbeNotify renders each pending retry twice: a one-line note
// on stderr for humans (stdout stays machine-readable for --json) and a
// run.warn record for JSON logs (a no-op in text mode by construction of
// logRunWarn).
func makeLogseqProbeNotify(opts *options, command string) func(logseqProbeAttempt) {
	st := newStyler(errWriter(opts), opts.disableColors)
	return func(a logseqProbeAttempt) {
		msg := logseqProbeRetryMessage(a)
		_, _ = fmt.Fprintln(errWriter(opts), st.warning(msg))
		attrs := map[string]any{
			"endpoint":        displayLogseqEndpoint(a.Source),
			"attempt":         a.Attempt,
			"attempts":        a.Attempts,
			"timeout_ms":      a.Timeout.Milliseconds(),
			"next_timeout_ms": a.NextTimeout.Milliseconds(),
			"error":           strings.TrimSpace(a.Err.Error()),
		}
		if a.Source.Name != "" {
			attrs["graph"] = a.Source.Name
		}
		logRunWarn(opts, command, "logseq.retry", msg, attrs)
	}
}

// logseqProbeRetryMessage is the human retry note: where, how long the
// failed attempt waited, which attempt it was, and what comes next.
func logseqProbeRetryMessage(a logseqProbeAttempt) string {
	where := displayLogseqEndpoint(a.Source)
	if a.Source.Name != "" {
		where = fmt.Sprintf("graph %q at %s", a.Source.Name, where)
	}
	return fmt.Sprintf("logseq: no answer from %s after %s (attempt %d of %d); retrying with a %s timeout...",
		where, a.Timeout, a.Attempt, a.Attempts, a.NextTimeout)
}

// logseqUnavailableError wraps the last Ping failure with where to look:
// the endpoint (and graph, in multi-graph mode), what the transport said,
// and the fix. Auth rejections name the token; everything else names the
// running app and api_url. TLS trust failures already carry the scheme
// hint from the adapter and are preserved verbatim inside the cause.
// Timeout causes name the final attempt's budget and the attempt count.
func logseqUnavailableError(s run.Source, err error, attempts int) error {
	cause := strings.TrimSpace(err.Error())
	if isLogseqTimeout(cause) {
		cause += fmt.Sprintf(" (timed out after %s, %d attempt(s))", logseqProbeTimeoutForAttempt(attempts-1), attempts)
	} else if attempts > 1 {
		cause += fmt.Sprintf(" (after %d attempt(s))", attempts)
	}
	if isLogseqAuthFailure(cause) {
		if s.Name != "" {
			return fmt.Errorf("graph %q: logseq denied access at %s: %s; check logseq.token (Logseq Settings → Integrations), %s, or pass --logseq-token / --logseq-graph-token %s=TOKEN",
				s.Name, displayLogseqEndpoint(s), cause, config.EnvLogseqToken, s.Name)
		}
		return fmt.Errorf("logseq denied access at %s: %s; check logseq.token (paste the Logseq API token from the app (Settings → Integrations), set %s, or pass --logseq-token)",
			displayLogseqEndpoint(s), cause, config.EnvLogseqToken)
	}
	if s.Name != "" {
		return fmt.Errorf("graph %q: logseq unavailable at %s: %s; is Logseq running with the HTTP API enabled (Settings → Integrations)? check logseq.api_url and logseq.token",
			s.Name, displayLogseqEndpoint(s), cause)
	}
	return fmt.Errorf("logseq unavailable at %s: %s; is Logseq running with the HTTP API enabled (Settings → Integrations)? check logseq.api_url and logseq.token",
		displayLogseqEndpoint(s), cause)
}

// displayLogseqEndpoint names the endpoint in diagnostics; sources built
// by hand in tests may carry none.
func displayLogseqEndpoint(s run.Source) string {
	if strings.TrimSpace(s.APIURL) != "" {
		return s.APIURL
	}
	return "(unknown api_url)"
}

// isLogseqAuthFailure reports whether the probe failure looks like a bad
// token rather than a dead app: the stub and the app answer 401/403 with
// an Access Denied body.
func isLogseqAuthFailure(cause string) bool {
	lower := strings.ToLower(cause)
	for _, want := range []string{"401", "403", "access denied", "unauthorized", "forbidden"} {
		if strings.Contains(lower, want) {
			return true
		}
	}
	return false
}

// isLogseqTimeout reports a probe that hit the gate's own deadline: the
// transport surfaces context deadline exceeded (or a timeout) rather than
// a refusal.
func isLogseqTimeout(cause string) bool {
	lower := strings.ToLower(cause)
	return strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timed out")
}
