---
title: "A missing Time Maps page is offered, never assumed"
weight: 150
---

> Status: **accepted, amended by [ADR 0017](./0017-tui-edits-logseq-blocks-on-explicit-save.md).**
> The bootstrap is no longer Lodo's only graph write. A validated TUI save may edit a
> Task. This record still governs the Time Maps page: offered, written only on an
> explicit yes, and never overwritten.

Without the configured Time Maps page, every Task without a `time-map::` has no
fallback hours, so a fresh graph fails its first `lodo check` with
`missing-time-maps-page` and no way forward inside the tool. When the page is
missing, Lodo offers to create it with the recommended defaults (a `Deep Work` map
marked `default:: true`, plus `Errands` and `Weekends`), which the user then edits
in Logseq. The offer needs an explicit yes — an interactive `y`, or `-y`/`--yes`
up front — and anything else leaves the graph untouched.

The bootstrap stays narrow. It creates a page that does not exist. It does not edit
the user's Tasks, and it does not overwrite an existing page. It does not write a
Schedule back; that rejection in
[ADR 0001](./0001-local-state-as-system-of-record.md) stands. The write travels the
same HTTP API as every read
([ADR 0002](./0002-logseq-via-http-api-only.md)).

Creating the page with no confirmation was rejected. That would write into the user's
notes on a routine run. Failing with no offer leaves a first run stuck on a page the
tool can create. A non-interactive run without `-y` declines, and end-of-input counts
as no, so cron does not hang on the question.

The recommended defaults stay parseable by the Time Map grammar they bootstrap. The
prompt and the `-y`/`--yes` flag belong to `check`, `forecast`, and `schedule`.
