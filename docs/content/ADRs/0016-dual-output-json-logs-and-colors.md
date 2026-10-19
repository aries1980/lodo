---
title: "Dual output: JSONL logs for machines, colors for humans"
weight: 160
---

Cron (or a manual run) calling `lodo schedule` is the deployment, and a
human reading the terminal is the other half. One stream cannot serve
both: cron needs timestamped, greppable, indexable records for Datadog
and Loki, while a human needs scannable color, alignment, and a summary
line.

Stdout keeps the result: human text by default, indented JSON with
`--json`. Stderr takes diagnostics: plain colored text by default,
JSONL with `--log-format=json` (or `LODO_LOG_FORMAT=json`) — one object
per line with `timestamp`, `level`, `message`, `service: lodo`,
`command`, `event` (`run.start`, `run.finish`, `calendar.warning`,
`run.error`), plus counts and `duration_ms`. The standard library
`slog` JSON handler produces it, with `time`/`msg` remapped to
`timestamp`/`message` for the collectors' conventions. No tokens are
logged; `--verbose` is `DEBUG`, otherwise `INFO`/`WARN`/`ERROR`.

Human text stays dependency-free ANSI: bold day headers, green
placed/synced, yellow shortfalls/warnings, red orphans/errors, dim busy
lines, tab-aligned times, and a `Summary:` footer. Colors die with
`--disable-colors`, `NO_COLOR`, `TERM=dumb`, or a non-TTY, so pipes,
tests, and cron mail stay plain.

Rejected alternatives: a color library buys little over a 50-line
wrapper and adds supply-chain weight to a single binary; logging the
result payload to stderr would break `--json` pipes; making `auto`
guess JSON on non-TTY would silently change scripted stderr and break
existing assertions, so `auto` stays `text` and CI opts in explicitly.
