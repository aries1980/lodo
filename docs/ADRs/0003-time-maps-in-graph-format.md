# Time Maps are defined in the graph via a line-based format

Lodo's Time Maps — named, weekly-recurring availability patterns — are authored in the
Logseq graph, on a page named `Time Maps`, rather than in a Lodo config file. Each Time
Map is a top-level block whose text is its name; each of its child blocks is a
human-readable window line (`Mon-Fri 09:00-11:00`) with an optional trailing preference
tier.

This keeps the graph as the single source of truth for everything the user authors,
which was the stated intent, and keeps Lodo's own configuration surface minimal.

Rejected alternatives: a Lodo config file would split the user's preferences across two
tools and two places they have to remember; encoding each window as a Logseq block
property expresses a repeated, multi-valued weekly grid poorly.

Consequences: Lodo must own a small bespoke parser for these lines, and the format is a
user-facing contract — changing it later means migrating the user's notes. Preference
tiers parse from day one but are all treated as green until preference fitting is built.
