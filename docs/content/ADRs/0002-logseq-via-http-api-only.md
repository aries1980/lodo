---
title: "Logseq is reached only through its HTTP API"
weight: 20
---

> Status: **accepted.**

Lodo reads and writes the Logseq graph exclusively through the Logseq HTTP API. It
never parses or edits the graph's Markdown files directly.

v0.1 has two writes, both on that API. One creates a missing Time Maps page when
the user confirms
([ADR 0015](./0015-bootstrap-missing-time-maps-on-confirmation.md)). The other saves
one block from the TUI after the validation gate
([ADR 0017](./0017-tui-edits-logseq-blocks-on-explicit-save.md)). That second write
is how intent authored in Lodo lands in the graph.

Direct file access was rejected because Logseq is a second writer to the same graph:
writes made behind a running Logseq's back can be overwritten or ignored until the
graph is re-indexed, and direct reads can silently diverge from the app's own index.
Going through the API keeps the app's state and Lodo's view consistent by construction.

The graph is currently file-based but is expected to move to Logseq's DB version.
Because the two flavours expose different APIs, the Logseq client is an interface with
one adapter per flavour, so that upgrade is a swap rather than a rewrite.

Consequence: Logseq must be running with the API server enabled and a token
configured. Lodo cannot operate headless against the graph.
