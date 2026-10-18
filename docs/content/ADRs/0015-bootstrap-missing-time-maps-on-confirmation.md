---
title: "A missing Time Maps page is offered, never assumed"
weight: 150
---


Without the configured Time Maps page every Task without a `time-map::` has
no fallback hours, so a fresh graph fails its first `lodo check` with
`missing-time-maps-page` and no way forward inside the tool. When the page is
missing, Lodo offers to create it with the recommended defaults (a `Deep Work`
map marked `default:: true`, plus `Errands` and `Weekends`), which the user
then edits in Logseq. The offer needs an explicit yes — an interactive `y`,
or `-y`/`--yes` up front — and anything else leaves the graph untouched.

This is Lodo's one graph write, and it stays narrow on purpose. It creates a
page that does not exist; it never edits the user's Tasks, Blocks, or
properties, never overwrites an existing page, and never writes a schedule
back (that rejection in [ADR 0001](./0001-local-state-as-system-of-record.md)
stands: reschedules still race the graph's own editor). The write travels
the same HTTP API as every read
([ADR 0002](./0002-logseq-via-http-api-only.md)).

Rejected alternatives: creating the page silently would write to the user's
notes behind their back, against the read-only rule; failing hard keeps a
first run stuck on an issue the tool could have fixed. A non-interactive run
without `-y` simply declines (end-of-input counts as no), so cron never
hangs on the question.

Consequences: the Logseq client owns five small write methods beside its
reads; the CLI owns the prompt, the `-y`/`--yes` flag on
`check`/`forecast`/`schedule`, and the recommended defaults, which must stay
parseable by the Time Map grammar they bootstrap.
