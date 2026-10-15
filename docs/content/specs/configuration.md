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
  id: primary           # the single output Calendar
```

- **`scheduling.granularity`** — the quantum of the Schedule. A whole number of minutes
  that divides 60 (e.g. `15m`, `20m`, `30m`). See
  [`tasks.md`](./tasks.md#splitting-into-blocks).
- **`scheduling.window_days`** — the rolling horizon. Nothing beyond it is scheduled or
  synced.
- **`scheduling.timezone`** — the timezone Time Map hours are interpreted in. Time Map
  hours are absolute local times, so this is what "09:00" means.
- **`calendar.id`** — the one Google calendar Lodo writes to. Time Maps are not calendars.
- **`logseq.insecure`** — skip TLS verification for an app serving a self-signed
  certificate. It is machine-bound, so it lives in this file rather than a flag: writing
  `true` here is the acceptance of that one certificate.

## `credentials.json`

```json
{
  "logseq": { "token": "…" },
  "google": { "refresh_token": "…", "client_id": "…", "client_secret": "…" }
}
```

Never committed, never synced, never written into the graph. The OAuth flow is
device/loopback-based and writes the refresh token here on first run.

## Overrides

Any setting may be overridden for a single run by a CLI flag (e.g.
`--window-days 30`, `--timezone America/New_York`). Overrides are not persisted.

## What is *not* here

- **Time Maps** — authored in the graph, on the `Time Maps` page ([ADR 0003](../ADRs/0003-time-maps-in-graph-format.md)).
- **The default Time Map** — marked on the map's own block in the graph (`default:: true`),
  because it is a property of a Time Map, not of Lodo.
