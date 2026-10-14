# Time Maps

> Status: **draft (v0.1).** Definition of the term lives in [`glossary.md`](./glossary.md).

A Time Map is a named, weekly-recurring pattern of hours in which the user is willing to
do a particular category of work. It answers *which hours of the week*, independent of
*when by* (Plan) and *exactly when* (Pin).

## Authoring

Time Maps are authored in the Logseq graph, on a page named **`Time Maps`** (see
[ADR 0003](../ADRs/0003-time-maps-in-graph-format.md)).

```markdown
- Deep Work
  - Mon-Fri 09:00-11:00
  - Sat 10:00-12:00 yellow
- Errands
  - Sat 10:00-13:00
- Weekends
  - Sat-Sun 08:00-20:00 yellow
```

## Schema

- **One top-level block = one Time Map.** The block's text is the map's name. The name is
  slugified, case-insensitively, into a key (`Deep Work` → `deep-work`).
- **Each child block = one window.** A window is a human-readable line: days, then a
  start–end time, then an optional trailing tier word.
- A map with no child blocks is empty and schedules nothing.

### Window grammar

```text
window := <days> <HH:MM>-<HH:MM> [tier]
days   := <day> | <day>-<day> | <day>,<day>,...
day    := Mon | Tue | Wed | Thu | Fri | Sat | Sun
tier   := green | yellow | red        ; optional; absent = green
```

- `Mon-Fri 09:00-11:00` — weekday mornings.
- `Mon,Wed,Fri 18:00-19:00` — listed days.
- `Sat-Sun 08:00-20:00 yellow` — a tiered weekend window.

### Tiers

Tiers parse from day one but are **all treated as green in v0.1**. SkedPal's
green/yellow/red gradient — fill green first, spill to yellow, then red, never blank —
requires preference fitting in the scheduler and is deferred. The schema already carries
the words so the notes do not need migrating when tiers are built.

## Assignment

- A Task may belong to one or more Time Maps, declared on the Task (or inherited from its
  page) as `time-map::`. Multiple maps are comma-separated: `time-map:: deep-work, weekends`.
- A Task is scheduled **only** inside the union of its maps' windows.
- **Default map (proposed):** the map block may carry `default:: true`. A Task with no
  `time-map::` (direct or inherited) uses the default map. Exactly one default must exist.

## Semantics (v0.1)

- Windows recur weekly and are **absolute local times**: they stay 09:00–11:00 in the
  user's current timezone, mirroring SkedPal's stated behaviour.
- Maps may overlap. A Task with several maps draws on the union of their windows;
  competition for the same hours between Tasks is resolved by Priority.
- Windows are intersected with the Schedule's busy time (External Events and Corporate
  Pins) and with the Plan window; a Task is placed only in hours that satisfy all three.

## Deferred

- Preference tiers (green/yellow/red fitting).
- Amending a map with calendar events (SkedPal's "amend with calendar events").
- Per-week customisation of hours.
