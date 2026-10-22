# Lodo

A personal, local-first scheduler for a Logseq TODO list. Lodo reads Tasks from a Logseq
graph, decides *when* to work on them, and projects the resulting schedule onto Google
Calendar.

> Status: **early implementation.** The Go binary is under construction from the
> specification in [`docs/`](docs/).

## How it fits together

Logseq is where intent lives (Tasks, Time Maps) and is the only interface. Lodo is a CLI
that computes a schedule and writes it to one Google Calendar. Local SQLite state is the
source of truth for the schedule; the calendar is a projection of it.

```mermaid
flowchart LR
    LS[("Logseq graph<br/>Tasks, Time Maps")]
    CFG[/"config.yaml + credentials"/]
    CAL[("Google Calendar")]
    LODO{{"Lodo CLI"}}
    DB[("SQLite")]
    LS -->|read-only| LODO
    CFG --> LODO
    CAL -->|busy time + user edits| LODO
    LODO -->|Managed Events| CAL
    LODO <-->|assignments| DB
```

## Development

All tooling runs in containers via podman, so there is nothing to install locally beyond
podman itself — no Go, no Hugo, no `make`. [`scripts/dev.sh`](scripts/dev.sh) is the
entry point:

```sh
./scripts/dev.sh build   # build the CLI to ./dist/lodo
./scripts/dev.sh test    # go test ./...
./scripts/dev.sh vet     # go vet ./...
./scripts/dev.sh fmt     # gofmt ./cmd ./internal
./scripts/dev.sh lint    # golangci-lint
./scripts/dev.sh tidy    # go mod tidy
```

The containers run as your own user (rootless podman remaps the UID with
`--userns=keep-id`, and `--user` overrides whatever `USER` the image declares), so files
written by the tools — build output, module caches under `/tmp`, the fetched docs theme —
are owned by you, not root.

## Releases

A tag of the form `0.1.1` — no `v` prefix — triggers
[`.github/workflows/release.yml`](.github/workflows/release.yml). It cross-compiles the CLI for
Linux, macOS and Windows on both amd64 and arm64, and publishes them as a GitHub Release. The
Go version comes from `go.mod`, so there is no second place to pin it.

| Asset | What it is |
| --- | --- |
| `lodo_<version>_<os>_<arch>.tar.gz` | The binary, plus `LICENSE` and `README.md` |
| `lodo_<version>_<os>_<arch>.spdx.json` | SPDX SBOM of the matching archive |
| `SHA256SUMS` | SHA-256 of every archive |
| `*.sigstore.json` | Keyless Sigstore signature bundle for the archive or the checksums |

Windows archives are gzipped tarballs like the others: Windows 10 and later ship `tar`, so
`tar -xzf lodo_<version>_windows_amd64.tar.gz` is all it takes.

### Verifying a download

```sh
# 1. the archive matches the published checksums
sha256sum -c SHA256SUMS

# 2. the checksums were signed by this repository's release workflow
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity-regexp 'https://github\.com/aries1980/lodo/\.github/workflows/release\.yml@refs/tags/.*' \
  --certificate-oidc-issuer 'https://token\.actions\.githubusercontent\.com' \
  SHA256SUMS

# 3. the archive was built from this commit, and its SBOM matches
gh attestation verify lodo_<version>_linux_amd64.tar.gz --repo aries1980/lodo
```

The first two steps need `sha256sum` and `cosign` only; the third needs the GitHub CLI. Every
archive also carries its own signature bundle, verifiable with the same `cosign verify-blob`
command against that archive's `.sigstore.json`.

## Documentation

The documentation is a self-contained Hugo site under [`docs/`](docs/): content in
`docs/content/`, config in `docs/hugo.toml`. The geekdoc theme is pinned in
[`scripts/dev.sh`](scripts/dev.sh) and fetched into the gitignored `docs/themes/` on
demand. The pages:

- [`docs/content/specs/glossary.md`](docs/content/specs/glossary.md) — the language. Start here.
- [`docs/content/specs/overview.md`](docs/content/specs/overview.md) — concept map and pipeline.
- [`docs/content/specs/scope.md`](docs/content/specs/scope.md) — what's in v0.1 and what's deferred.
- [`docs/content/specs/tasks.md`](docs/content/specs/tasks.md) — what a Task is and how it's written in Logseq.
- [`docs/content/specs/time-maps.md`](docs/content/specs/time-maps.md) — preferred hours, in the graph.
- [`docs/content/specs/scheduling.md`](docs/content/specs/scheduling.md) — ordering, objective, infeasibility.
- [`docs/content/specs/calendar.md`](docs/content/specs/calendar.md) — Google Calendar integration.
- [`docs/content/specs/logseq-integration.md`](docs/content/specs/logseq-integration.md) — reading the graph.
- [`docs/content/specs/configuration.md`](docs/content/specs/configuration.md) — config and secrets.
- [`docs/content/specs/cli.md`](docs/content/specs/cli.md) — commands and flags.
- [`docs/content/ADRs/`](docs/content/ADRs/) — the decisions worth remembering, with reasons.

## Previewing the documentation site

```sh
./scripts/dev.sh docs        # live server on http://localhost:1313/lodo/
./scripts/dev.sh docs-build  # one-off minified build into docs/public/
./scripts/dev.sh docs-theme  # only (re)fetch the pinned theme
```

The first two use the same `ghcr.io/gohugoio/hugo` image as CI — see
[`.github/workflows/hugo.yml`](.github/workflows/hugo.yml) for the current tag (currently
`v0.168.0`). `docs-theme` downloads the pinned geekdoc release tarball, verifies its
checksum, and extracts it into `docs/themes/hugo-geekdoc/`; the directory is gitignored.
The `--bind 0.0.0.0` needed to make the published port reachable is wired into the script.

Then open <http://localhost:1313/lodo/> — note the `/lodo/` subpath, which comes from
`baseURL` in `hugo.toml`.
