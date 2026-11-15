---
title: "Dual output: JSONL logs for machines, colors for humans"
weight: 160
---

> Status: **accepted.** Field names, colors, and the `--json` schema live in
> [cli.md](../specs/cli.md#output-modes).

Cron calling `lodo schedule` is one audience. A person reading the terminal is the
other. Stdout carries the result: human text by default, JSON with `--json`. Stderr
carries diagnostics: colored text by default, and JSONL only when logging is switched
to JSON explicitly (`--log-format=json` or `LODO_LOG_FORMAT=json`). Tokens are not
logged.

Colors follow the terminal. They turn off with `--disable-colors`, `NO_COLOR`,
`TERM=dumb`, or a non-TTY, so pipes, tests, and cron mail stay plain.

A color library was rejected. The palette is small, and a dependency is weight on the
single binary ([ADR 0009](./0009-lodo-is-a-single-go-binary.md)). Putting the result
payload on stderr was rejected. It would break a `--json` pipe. Guessing JSON logs
from a non-TTY was rejected. Scripted stderr would change shape with no explicit
opt-in, so the default stays human text and CI opts in.
