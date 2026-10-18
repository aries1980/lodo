---
title: "Configuration"
weight: 90
---


> Status: **draft (v0.1).**

Lodo's configuration lives **outside** the Logseq graph. The graph holds what the user
authors (Tasks, Time Maps); the config file holds how Lodo runs, and everything secret.
See [ADR 0005](../ADRs/0005-config-and-secrets-outside-the-graph.md).

The deciding rule for any future setting:

> **Secret or machine-bound → file. User intent about work → graph.**

## Files

| File | Contents | Permissions |
| --- | --- | --- |
| `~/.config/lodo/config.yaml` | non-secret settings | normal |
| `~/.local/state/lodo/credentials.json` | Logseq API token, Google OAuth token | `0600` |
| `~/.local/state/lodo/lodo.db` | the [Schedule state](./overview.md) (SQLite) | normal |

Paths follow the XDG base-directory convention. Both state files default under
`~/.local/state/lodo/` so a `rm -rf ~/.local/state/lodo` resets Lodo without touching
notes or config.

## `config.yaml`

```yaml
logseq:
  api_url: http://127.0.0.1:12315/api
  time_maps_page: Time Maps   # configurable so `lodo/time-maps` is a one-line change
  insecure: false             # true for an app serving a self-signed certificate

scheduling:
  granularity: 15m      # the calendar's quantum, and the floor for small Tasks
  window_days: 14       # rolling Scheduling window
  timezone: Europe/London

calendar:
  provider: google
  id: primary           # the single write Calendar; v0.1 writes to exactly one
  read_calendars:       # shared calendars read for busy time only; add as many as needed
    - partner@example.com                              # person share: owner email
    - team@group.calendar.google.com                   # group share: group ID
    - resource_123@resource.calendar.google.com        # resource: resource ID
  ignore_read_all_day: false          # true: all-day events on read calendars are free
  ignore_read_all_day_matching: []    # or phrases, e.g. birthday; all-day events only
```

- **`scheduling.granularity`** — the quantum of the Schedule. A whole number of minutes
  that divides 60 (e.g. `15m`, `20m`, `30m`). See
  [`tasks.md`](./tasks.md#splitting-into-blocks).
- **`scheduling.window_days`** — the rolling horizon. Nothing beyond it is scheduled or
  synced.
- **`scheduling.timezone`** — the timezone Time Map hours are interpreted in. Time Map
  hours are absolute local times, so this is what "09:00" means.
- **`calendar.id`**: the one Google calendar Lodo writes to. Time Maps are not calendars.
- **`calendar.read_calendars`**: list of shared Google calendars read for busy time only
  (owner email for person shares, resource or group ID otherwise). Add one entry
  per calendar. Empty means the write calendar alone. Lodo never writes to these,
  even if the share grants write access. Entries are trimmed; empties, duplicates,
  and entries equal to `calendar.id` are ignored.
- **`calendar.ignore_read_all_day`**: when true, all-day events on read calendars
  contribute no busy time. Default false. The write calendar is unchanged.
- **`calendar.ignore_read_all_day_matching`**: phrases. An all-day event on a read
  calendar whose summary contains one (case-insensitive) is free. Timed events are
  never matched. Unused when `ignore_read_all_day` is true. Entries are trimmed,
  lowercased, and de-duplicated.
- **`logseq.insecure`** — skip TLS verification for an app serving a self-signed
  certificate. It is machine-bound, so it lives in this file rather than a flag: writing
  `true` here is the acceptance of that one certificate.

## Multiple graphs

One endpoint always serves its current graph only, so each graph needs its
own Logseq window on its own HTTP port. List them under `logseq.graphs`
(in priority order — ties break by list order, then outline order):

```yaml
logseq:
  time_maps_page: Time Maps   # canonical page, read from graphs[0]
  graphs:
    - name: work
      api_url: http://127.0.0.1:12315/api
      default_time_map: deep-work
    - name: family
      api_url: http://127.0.0.1:12316/api
      default_time_map: weekends
```

- **`name`** — graph slug (`work`, `family`). Shown in output as `[work]`,
  stored on Managed Events as `graph`, and used for token lookup. Unique,
  case-insensitively.
- **`api_url`** — this graph HTTP API endpoint. Required per graph; the
  top-level `logseq.api_url` is then optional (single-graph mode uses it).
- **`default_time_map`** — graph-level fallback Time Map slug. Explicit
  `time-map::` on a block, parent block, or page wins over it; it wins over
  the global `default:: true` map. Must name a map from the canonical page.
- **`time_maps_page` per graph** is reserved (defaults to the global page).
  The canonical Time Maps are always read from the first graph; other graphs
  `Time Maps` pages are skipped for tasks and contribute no maps.
- **`depends::` and `order::` stay graph-local.** A ref to another graph
  reports `unknown-dependency` and blocks — cross-graph chains are unsupported.

## `credentials.json`

```json
{
  "logseq": { "token": "…" },
  "google": { "refresh_token": "…", "client_id": "…", "client_secret": "…" }
}
```

Per-graph tokens (file shape mirrors the graph names):

```json
{
  "logseq": {
    "token": "…",
    "graphs": {
      "work": { "token": "…" },
      "family": { "token": "…" }
    }
  }
}
```

Resolution per graph: `LODO_LOGSEQ_TOKEN_<GRAPH>` wins (e.g.
`LODO_LOGSEQ_TOKEN_WORK`), then the per-graph file entry
(case-insensitive), then the global token (file, env, or
`--logseq-token`). A missing token for a configured graph fails the run
naming the graph and its env var.

Never committed, never synced, never written into the graph. The OAuth flow is
device/loopback-based and writes the refresh token here on first run.

Every secret is also passable as an environment variable, which wins per field
over the file. A missing file plus a complete environment runs without a file,
which is what containers and CI use:

| Environment variable | File field it replaces |
| --- | --- |
| `LODO_LOGSEQ_TOKEN` | `logseq.token` |
| `LODO_GOOGLE_CLIENT_ID` | `google.client_id` |
| `LODO_GOOGLE_CLIENT_SECRET` | `google.client_secret` |
| `LODO_GOOGLE_REFRESH_TOKEN` | `google.refresh_token` |

`lodo auth google` accepts the client pair from the environment and still
stores the granted refresh token in the file; an environment-only Logseq token
is never copied into the file.

## Precedence

Lowest to highest, per field, for one run. Nothing but the files persists:

1. Files: `config.yaml` and `credentials.json` are the base.
2. Environment: `LODO_*` variables win per field over `credentials.json`.
3. CLI flags win over everything before them (file and environment):

   | CLI flag | Replaces |
   | --- | --- |
   | `--logseq-token` | `LODO_LOGSEQ_TOKEN` / `logseq.token` |
   | `--google-client-id` | `LODO_GOOGLE_CLIENT_ID` / `google.client_id` |
   | `--google-client-secret` | `LODO_GOOGLE_CLIENT_SECRET` / `google.client_secret` |
   | `--google-refresh-token` | `LODO_GOOGLE_REFRESH_TOKEN` / `google.refresh_token` |
   | `--window-days`, `--timezone`, `--read-calendar` | `scheduling.*`, `calendar.read_calendars` |

In other words: files first, environment overrides the files, command-line
flags always overwrite whatever was set before. `lodo auth google
--google-refresh-token` with the client pair stores the grant directly
without running the browser flow.

{{< hint type="warning" title="Secrets on the command line" >}}
Flags suit one-off runs; they can surface in shell history or `ps`,
so environment or files remain the default for routine use.
{{< /hint >}}

## Overrides

Settings and secrets with a CLI twin may be overridden for a single run (e.g.
`--window-days 30`, `--logseq-token …`). Overrides are not persisted.
The repeatable `--read-calendar` flag replaces the `read_calendars` list for one
run when given at least once. See Precedence above for how files, environment,
and flags combine.

## What is *not* here

- **Time Maps** — authored in the graph, on the `Time Maps` page ([ADR 0003](../ADRs/0003-time-maps-in-graph-format.md)). If the page is missing, `check`/`forecast`/`schedule` offer to create it with recommended defaults on confirmation ([CLI](./cli.md#missing-time-maps-page)); the configured `logseq.time_maps_page` names the page in both cases.
- **The default Time Map** — marked on the map's own block in the graph (`default:: true`),
  because it is a property of a Time Map, not of Lodo.
