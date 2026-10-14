# Configuration and secrets live outside the graph

Lodo's settings live in a config file (`~/.config/lodo/config.yaml`) and its secrets in
a separate credentials file with `0600` permissions, not in the Logseq graph. Only
authored content — Tasks and Time Maps — belongs in the graph.

The driving constraint is not preference but safety: a Logseq graph syncs, gets shared,
and gets backed up. An OAuth refresh token or a Logseq API token written into a page is a
credential leak, so a file-based secret store is unavoidable regardless of where
preferences go. Once a config file exists, keeping the remaining settings there too
avoids two mechanisms for one job.

The deciding rule, for future settings: *secret or machine-bound → file; user intent
about work → graph.* This is why Time Maps (intent) are in the graph while granularity
and the scheduling window (machine policy) are not.

Consequence: the default Time Map is marked in the graph, on the map's own block, since
it is a property of a Time Map rather than of Lodo.
