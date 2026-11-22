---
title: "Backends"
weight: 75
---

> Status: **draft.**

Lodo reads Tasks and Time Maps from a **task backend** and projects the
Schedule onto one Google Calendar. The backend holds user intent about
work; the calendar is a projection of the Schedule, never a second source
of intent.

Three backends exist. They are interchangeable at the Schedule level:
resolution, planning, reconciliation and the store never know which one a
Task came from.

| Backend | Source | Account | How Lodo talks to it |
| --- | --- | --- | --- |
| Logseq | A Logseq graph over its HTTP API | n/a (local app) | `logseq.*` config + API token |
| Google Keep Workspace (`keep-workspace`) | Keep notes via the official Keep REST API | Paid Google Workspace | Same Google OAuth grant as Calendar + `keep` scope |
| Google Keep Personal (`keep-personal`, experimental) | Keep notes via a token-authenticated sync transport | Free personal `@gmail.com` | Sync token + `keep.sync_url` |

The split is by account type because the two Keep access paths are
entirely different protocols. The official API rejects personal accounts
with `400 invalid_scope` and no workaround; the unofficial transport
works for personal accounts but is reverse-engineered and may break on
Google-side changes. See [ADR 0026](../ADRs/0026-task-backends-behind-a-graph-seam.md)
for why the seam looks the way it does.

## Choosing one

- **Logseq** stays the default: full hierarchy (containers, sequential
  chains, ancestor inheritance), native markers and properties, local
  HTTP reads. Use it unless you want your Tasks in Keep.
- **Keep Workspace** fits a Workspace account whose notes already live in
  Keep. Checklist items are Tasks; one label-filtered note set is one
  source. No hierarchy: every item is top-level.
- **Keep Personal** fits a personal `@gmail.com` account with the same
  mapping and the same flatness, plus the experimental caveat: budget for
  re-setup when Google changes the sync protocol.

Backends mix freely: `logseq.graphs` and `keep.sources` merge into one
run, ordered Logseq-first, then Keep. Canonical Time Maps still come from
the first source; cross-source `depends::`/`order::` stay unsupported,
like cross-graph chains today.

## Keep limitations (both flavours)

Keep has no hierarchy, no markers beyond checked/unchecked, no per-item
ids you choose, and no page properties. The full list lives in
[`keep-integration.md`](./keep-integration.md#unsupported-on-keep): read
it before moving Tasks to Keep, because several Logseq shapes
(containers, sequential chains, `DOING`/`CANCELLED`, `depends::` across
notes) do not survive the move.

## Setup

- Logseq: [`logseq-integration.md`](./logseq-integration.md) and
  [`configuration.md`](./configuration.md).
- Keep Workspace and Keep Personal, step by step:
  [`keep-integration.md`](./keep-integration.md).
