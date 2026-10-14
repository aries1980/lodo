# Logseq is reached only through its HTTP API

Lodo reads and writes the Logseq graph exclusively through the Logseq HTTP API. It
never parses or edits the graph's Markdown files directly.

Direct file access was rejected because Logseq is a second writer to the same graph:
writes made behind a running Logseq's back can be overwritten or ignored until the
graph is re-indexed, and direct reads can silently diverge from the app's own index.
Going through the API keeps the app's state and Lodo's view consistent by construction.

The graph is currently file-based but is expected to move to Logseq's DB version.
Because the two flavours expose different APIs, the Logseq client is an interface with
one adapter per flavour, so that upgrade is a swap rather than a rewrite.

Consequence: Logseq must be running with the API server enabled and a token
configured. Lodo cannot operate headless against the graph.
