---
title: "Getting Started"
weight: 6
---

From zero to the first `schedule` in about ten minutes.

Prereqs: Logseq running, a Google Cloud OAuth client, `podman`.

## 1. Build

All tooling runs in containers — no local Go install needed:

```sh
./scripts/dev.sh build   # CLI to ./dist/lodo
./scripts/dev.sh test    # go test ./...
./scripts/dev.sh docs    # live docs on http://localhost:1313/lodo/
```

## 2. Logseq — token and Time Maps

1. In Logseq: Settings → Features → Developer mode → enable **HTTP APIs server**,
   copy the token.
2. Keep Logseq running. Lodo reads only through the HTTP API
   (see [Logseq Integration](specs/logseq-integration.md)).
3. No `Time Maps` page yet? `lodo check` offers to create it with recommended
   defaults (`Deep Work` marked `default:: true`, plus `Errands` and `Weekends`)
   on explicit confirmation (`-y` for non-interactive). That is the one graph
   write Lodo ever makes.

Time Map format and tiers: [Time Maps](specs/time-maps.md).

## 3. Google — one-time OAuth

Create a **Desktop app** OAuth client (Calendar API enabled, redirect
`http://127.0.0.1/`), paste the pair into the credentials file, then:

```sh
dist/lodo auth google
```

Headless machine: `auth google --no-launch-browser`, finish the URL on any
device, paste the redirect back. Full steps in
[Calendar Integration](specs/calendar.md#auth).

## 4. Config + credentials

Paths: `~/.config/lodo/config.yaml`,
`~/.local/state/lodo/credentials.json` (`0600`),
`~/.local/state/lodo/lodo.db` (SQLite). `rm -rf ~/.local/state/lodo` resets
without touching notes or config.

```yaml
logseq:
  api_url: http://127.0.0.1:12315/api
  time_maps_page: Time Maps

scheduling:
  granularity: 15m
  window_days: 14
  timezone: Europe/London

calendar:
  provider: google
  id: primary
  read_calendars: []
```

```json
{
  "logseq": { "token": "…" },
  "google": { "client_id": "…", "client_secret": "…" }
}
```

Secrets also work as env vars (`LODO_LOGSEQ_TOKEN`, `LODO_GOOGLE_CLIENT_ID`,
`LODO_GOOGLE_CLIENT_SECRET`, `LODO_GOOGLE_REFRESH_TOKEN`) and one-run flags
(`--logseq-token`, `--window-days`, `--read-calendar`, …). Overrides are never
persisted. Full table: [Configuration](specs/configuration.md).

Multiple graphs (work / family / personal)? Give each Logseq window its own
HTTP port and list them under `logseq.graphs` with a `default_time_map` each —
see [Features](features.md#multiple-graphs). One endpoint always serves its
current graph only.

## 5. First Task

In Logseq, new block, type `TODO`, add `duration::`. No duration = note.

```
TODO Write deep report
duration:: 2h
time-map:: deep-work
priority:: B
plan-end:: 2026-10-24
```

Save a Logseq template (`/Template`) for this. Full entry guide with
`depends::`, nesting, and page defaults:
[Entering Tasks](../guides/entering-tasks.md).

## 6. Check → forecast → schedule

```sh
dist/lodo check     # graph issues + shortfalls + stood-down + orphans
dist/lodo forecast  # dry run: Blocks by day, shortfalls, no writes
dist/lodo schedule  # reconcile first, then create/move Managed Events
dist/lodo refresh   # fold calendar moves/deletes into state, no recompute
```

`--json` for machine-readable output, `--verbose` for placement reasons,
`--strict` to fail on reported problems (exit 2). Flags:
[CLI](specs/cli.md).

A moved Managed Event becomes a **Pinned Block**; a deleted one **stands down**
until the Task's inputs change. Both survive unrelated edits.

## 7. Make it recurring

```sh
# crontab: recompute every 15 minutes
*/15 * * * * /path/to/lodo schedule
```

`schedule` always reconciles before computing, so a calendar move made between
runs is never clobbered.

## Troubleshooting

* **Logseq unreachable** — Logseq must be running with the API server on;
  check `logseq.api_url` scheme (`http://127.0.0.1:12315/api` by default) and token.
* **Calendar auth failed** — re-run `auth google` with the account whose
  calendar `calendar.id` names; enable the Calendar API on the owning project.
* **Nothing scheduled** — run `check`: missing `duration::`, unknown
  `time-map::` slug, or hours outside every Time Map window.
* **Partial mask warning** — a `403`/`404` read calendar was skipped; placement
  continued on the rest and printed `busy sources:` so it stays visible.
