---
title: "Keep Integration"
weight: 81
---

> Status: **draft.**

Google Keep as a task backend, in two flavours: `keep-workspace` (paid
Google Workspace accounts, official REST API) and `keep-personal` (free
personal accounts, token sync transport, experimental). They share one
mapping and differ only in transport. See [`backends.md`](./backends.md)
for how to choose, and [ADR 0026](../ADRs/0026-task-backends-behind-a-graph-seam.md)
for the architecture.

## Direction: reads, plus two explicit writes

Same contract as Logseq: Lodo reads Keep notes, with two write paths,
both explicit. When the configured Time Maps note is missing,
`check`/`forecast`/`schedule` offer to create it with the recommended
defaults, on explicit confirmation (interactively, or up front with
`-y`/`--yes`). And the TUI saves a full block through the backend only
after the user quits `$EDITOR` and the edit passes the per-Task
validation gate. Nothing else writes Tasks or properties. See
[ADR 0015](../ADRs/0015-bootstrap-missing-time-maps-on-confirmation.md)
and [ADR 0017](../ADRs/0017-tui-edits-logseq-blocks-on-explicit-save.md).

## Mapping: checklist-item-per-task

- One Keep **note** is one page: the note title is the page name, so
  titles must be unique within a source (two notes titled alike fail the
  run naming both).
- One **checklist item** is one Task-candidate block: unchecked is
  `TODO`, checked is `DONE`. The block UUID is `{noteID}#{itemID}`,
  stable across runs because both halves are Google-issued.
- Item text carries the same `duration::`, `time-map::`, `priority::`,
  `plan-start::`, `plan-end::`, `scheduled::`, `depends::`, `order::`
  lines Logseq blocks carry; parsing is identical. A `DOING` or
  `CANCELLED` text marker stays visible but the checked state decides:
  checked items read as complete, exactly like the DONE/CANCELLED gate.
- Notes carry no page properties, and items have no parents, so
  inheritance is page-level at most and in practice absent: put every
  property the Task needs on the item itself.
- The **Time Maps note** (titled with `logseq.time_maps_page`, default
  `Time Maps`) is a *text* note, not a checklist: one top-level line per
  map, one indented line per window, `default:: true` on exactly one map.
  The adapter parses the indentation back into a tree, so Time Map
  windows keep working where task notes stay flat.
- `depends:: ((uuid))` references the synthesized `{noteID}#{itemID}`
  form; cross-note and cross-source refs report `unknown-dependency`,
  like cross-graph chains.

## Access

One note set per source, filtered by Keep label: `label: lodo` reads only
notes carrying that label; empty reads every untrashed, unarchived note.
Sources merge in config order after the Logseq graphs; Time Maps always
come from the first source overall.

## Setup: Keep Workspace (paid account)

1. In the Google Cloud project that owns your OAuth client, **enable the
   Keep API** (APIs & Services → Enable APIs → Google Keep API).
2. Re-run the grant so the new scope is consented:
   `lodo auth google` (same loopback flow as Calendar; the authorization
   URL now asks for the `keep` scope too). A Workspace admin's
   third-party app policy must allow your own client.
3. Label the notes Lodo should read in Keep (e.g. `lodo`), and title one
   of them `Time Maps` — or let Lodo create it on confirmation.
4. Configure one source per note set:
   ```yaml
   keep:
     sources:
       - name: personal
         backend: keep-workspace
         label: lodo
   ```
5. Run `lodo check`. A `keep denied access` naming the keep scope means
   step 2 has not been re-run; a `400 invalid_scope` on a personal
   `@gmail.com` means this flavour cannot work there — use Keep Personal.

## Setup: Keep Personal (free account, experimental)

1. Create an app password on the Google account (Account → Security →
   2-Step Verification → App passwords) and mint the sync token per the
   printed flow; store it as `LODO_KEEP_TOKEN` (or per source,
   `LODO_KEEP_TOKEN_<SOURCE>`), or in `credentials.json` under
   `keep.token` / `keep.sources.<name>.token`.
2. Set the sync endpoint root: `keep.sync_url` in `config.yaml`, or
   `LODO_KEEP_SYNC_URL` for one environment. Without it every Keep
   command fails closed naming this page and issue #29 — it never dials
   a guessed host.
3. Same `sources` shape with `backend: keep-personal`, same label rule,
   same Time Maps title.
   ```yaml
   keep:
     sync_url: https://<sync-host>
     sources:
       - name: personal
         backend: keep-personal
         label: lodo
   ```
4. Run `lodo check`. Expect breakage across Google-side changes: the
   transport is reverse-engineered, and a dead sync is an operational
   error with a fix hint, never a silently empty plan.

## Unsupported on Keep

Backend limits, not bugs — Lodo reports instead of guessing:

- No hierarchy: no containers (a Task with Task children), no `order::
  sequential` chains, no nearest-ancestor inheritance. Every checklist
  item is top-level.
- No page properties: `time-map::`, `priority::`, `plan-start/end::`
  defaults cannot be set per note; each item carries its own.
- No `DOING`/`CANCELLED` states: unchecked is TODO, checked is DONE.
  The words survive in text but change nothing.
- No stable author-chosen ids: block identity is `{noteID}#{itemID}`,
  so `depends::` is hand-written against Google-issued ids and only
  within one note.
- No cross-note `depends::`/`order::`: refs to another note report
  `unknown-dependency`, like cross-graph chains.
- Time Maps note only: outline text, not checklists; block ids there
  are session-stable (`{noteID}#L{n}`), not cross-run.
- Keep reminders, attachments, collaborators, colors and archive/trash
  states beyond the trashed/archived exclusion are ignored. Trashed and
  archived notes are never read.
- Personal flavour only: the sync transport may break without warning;
  re-setup follows the same steps, and failures always name the docs.
