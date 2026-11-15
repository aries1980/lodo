---
title: "The TUI edits Logseq blocks on explicit save"
weight: 170
---

> Status: **accepted.**

Bare `lodo` on an interactive terminal opens a TUI that edits the user's Tasks.
Intent can be authored there, as well as in Logseq. Saving from `$EDITOR` writes
the full Logseq block back through `logseq.Editor.update_block`, after a per-Task
validation gate. The graph holds that block afterwards, and a later `schedule`
reads it from there. The store holds the Schedule, not a second copy of the Task.
This is Lodo's second graph write, beside the Time Maps bootstrap
([ADR 0015](./0015-bootstrap-missing-time-maps-on-confirmation.md)), and it is the
write that may edit a Task.

The gate is the decision: the edited block is overlaid onto an in-memory copy of
its graph and rerun through `resolve` before any write. Any issue naming that
Task or block — `bad-property-value`, `todo-missing-duration`,
`unknown-time-map`, `unknown-dependency`, `dependency-cycle` — blocks the save.
`unknown-property` stays advisory. A whole-graph clean is not required, so
unrelated graph problems never trap an edit.

Saving the temp file with no validation was rejected. It would break the duration
gate ([ADR 0004](./0004-duration-is-the-gate.md)) and show up later as `check`
failures. Validating only the edited text in isolation was rejected. It misses
`time-map::`, `priority`, and `plan` inherited from parents and the page,
cross-block `depends::`, and container rules. An in-TUI form as the primary
editor was rejected. It is more code, a worse editor than `$EDITOR`, and it still
needs the same gate. A TUI that plans, or that reads the calendar and the store,
was rejected. Authoring a Task belongs here. Computing the Schedule stays with
`forecast` and `schedule`.

The TUI re-resolves after every save and keeps the cursor on the edited Task.
Demoting a Task out of schedulability is not a valid TUI edit in v0.1. The TUI
does not plan and does not touch the calendar or the store. Creating a missing
Time Maps page stays on `check`, `forecast`, and `schedule`. Logseq remains a
second writer, so a block edited in Logseq while the TUI is open is overwritten
by the TUI save. Last write wins, as it does between two Logseq windows.
