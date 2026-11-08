# Contributing to Lodo

Podman or docker is the only host requirement (plus `curl` or `wget` for the docs theme
download). No local Go, Hugo, or `make` — everything runs in containers via
[`scripts/dev.sh`](scripts/dev.sh), as the invoking user.

```sh
./scripts/dev.sh build      # build the CLI to ./dist/lodo
./scripts/dev.sh test       # go test ./...
./scripts/dev.sh vet        # go vet ./...
./scripts/dev.sh fmt        # gofmt ./cmd ./internal
./scripts/dev.sh lint       # golangci-lint
./scripts/dev.sh tidy       # go mod tidy
./scripts/dev.sh changelog  # regenerate CHANGELOG.md from history
./scripts/dev.sh docs       # live docs at http://localhost:1313/lodo/
./scripts/dev.sh docs-build # one-off minified build into docs/public/
```

## Before committing

At minimum: `fmt`, `vet`, and tests for affected packages; run `lint` when
practical. Keep files focused, preserve package boundaries in
[`CONTEXT.md`](CONTEXT.md#where-to-change-x), and prefer explicit contracts over
implicit global state.

## Commits and releases

Commits must follow conventional commits — `git-cliff` parses them
(`conventional_commits = true`, non-conforming commits are ignored per
[`cliff.toml`](cliff.toml)):

| Prefix | Group |
| --- | --- |
| `feat` | Features |
| `fix` | Bug fixes |
| `docs` | Documentation |
| `build`, `ci` | Maintenance |
| `chore` | omitted |

Do not edit `CHANGELOG.md` by hand; it is generated from history. A tag of the
form `0.1.1` (no `v` prefix) triggers
[`.github/workflows/release.yml`](.github/workflows/release.yml).

## Docs

- Specs are the source of truth: [`docs/content/specs/`](docs/content/specs/)
  (start at `glossary.md`, `overview.md`, `cli.md`, `scheduling.md`,
  `calendar.md`).
- Decisions live in [`docs/content/ADRs/`](docs/content/ADRs/) — cite stable IDs
  (e.g. ADR 0001, ADR 0007) in code when the choice is non-obvious.
- Adding a flag/command: wire the constructor + handler + tests in
  `internal/cli/*`. Task/Time Map/dependency shape changes go through
  `internal/resolve/*` + `internal/model/*`; persistence through
  `internal/store/*` (mind ADR 0007 write ordering).
