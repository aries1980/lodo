package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Semantic JSON logging for CI/cron (Datadog, Loki).
//
// Result payloads stay on stdout (--json). Diagnostics go to stderr as
// JSONL: one object per line with timestamp, level, message plus stable
// attributes (service, command, event, duration_ms, counts). Text mode
// (the default) prints nothing extra, preserving the existing human
// output byte-for-byte.

const logService = "lodo"

// resolveLogFormat normalizes --log-format / LODO_LOG_FORMAT.
// Accepted: auto, text, json. auto is currently text: explicit opt-in
// keeps existing scripts and tests stable; CI sets --log-format=json
// or LODO_LOG_FORMAT=json.
func resolveLogFormat(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "json":
		return "json"
	default:
		return "text"
	}
}

func isJSONLog(opts *options) bool {
	return resolveLogFormat(opts.logFormat) == "json"
}

// logFormatDefault reads LODO_LOG_FORMAT, falling back to auto.
func logFormatDefault() string {
	if v := strings.TrimSpace(os.Getenv("LODO_LOG_FORMAT")); v != "" {
		return v
	}
	return "auto"
}

func newJSONLogger(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "timestamp"
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.UTC().Format(time.RFC3339Nano))
				}
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	})
	return slog.New(h).With("service", logService)
}

// emitJSON writes one JSONL record. attrs are flattened into the object.
func emitJSON(w io.Writer, verbose bool, level slog.Level, command, event, msg string, attrs map[string]any) {
	if w == nil {
		return
	}
	args := []any{"command", command, "event", event}
	for k, v := range attrs {
		args = append(args, k, v)
	}
	newJSONLogger(w, verbose).Log(context.Background(), level, msg, args...)
}

func logRunStart(opts *options, command string) time.Time {
	start := time.Now()
	if !isJSONLog(opts) {
		return start
	}
	emitJSON(errWriter(opts), opts.verbose, slog.LevelInfo, command, "run.start", command+" started", nil)
	return start
}

func logRunFinish(opts *options, command string, start time.Time, attrs map[string]any) {
	if !isJSONLog(opts) {
		return
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["duration_ms"] = time.Since(start).Milliseconds()
	emitJSON(errWriter(opts), opts.verbose, slog.LevelInfo, command, "run.finish", command+" finished", attrs)
}

func logRunWarn(opts *options, command, event, msg string, attrs map[string]any) {
	if !isJSONLog(opts) {
		return
	}
	emitJSON(errWriter(opts), opts.verbose, slog.LevelWarn, command, event, msg, attrs)
}

// logRunError emits the terminal error record; cli.Run calls it so every
// command shares the same shape, including failures before a command starts.
func logRunError(w io.Writer, verbose bool, command, msg string, code int, attrs map[string]any) {
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["exit_code"] = code
	emitJSON(w, verbose, slog.LevelError, command, "run.error", msg, attrs)
}
