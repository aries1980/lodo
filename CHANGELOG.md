# Changelog

All notable changes to Lodo, generated from the commit history by
[git-cliff](https://git-cliff.org).

## Unreleased

### Maintenance

- Generate the changelog and release notes from the commit history ([e73f10c](https://github.com/aries1980/lodo/commit/e73f10c54b0796df1899875132c71594081d1f50))
- Pin the Go toolchain in go.mod ([4cae433](https://github.com/aries1980/lodo/commit/4cae4336a1222fddc09808b5e29b234d33b68359))
- Release workflow for signed multi-arch binaries on tags ([f852769](https://github.com/aries1980/lodo/commit/f852769c222a5a73c36668eadbbf835343330108))

## 0.1.1 - 2026-10-22

### Maintenance

- Upgrade to Go 1.27.3 and golangci-lint 2.15.0 ([81f1169](https://github.com/aries1980/lodo/commit/81f11691724f698fcf725bfcfd8e385d48af54a6))

[Full changelog](https://github.com/aries1980/lodo/compare/0.1.0...0.1.1)

## 0.1.0 - 2026-10-19

### Features

- JSONL logs for CI/cron plus colored human output with summaries ([5d9bde3](https://github.com/aries1980/lodo/commit/5d9bde3f0a1a0771b413e12b4af11d80f06d39cf))
- Read multiple graphs with per-graph default time maps ([585862c](https://github.com/aries1980/lodo/commit/585862c94648c9ff456fff6af48ef8050d50e1ed))
- Never spill into a better tier than the project map ([2eccd76](https://github.com/aries1980/lodo/commit/2eccd76e645cded04c0b47ce3aad68d15e4f8d42))
- Project Time Maps spill tier-first into default ([3f7489b](https://github.com/aries1980/lodo/commit/3f7489bbfc68cdc215c54ddebd17a38052d91edf))
- Offer to create a missing Time Maps page on confirmation, -y to pre-answer ([abb44b0](https://github.com/aries1980/lodo/commit/abb44b07675ce4afab469fd956c15ec4b6d9e3c5))
- Pass credentials via env vars and secret CLI flags ([6c96937](https://github.com/aries1980/lodo/commit/6c969379845fe47c05215a105233a846c798bef4))
- Fit Time Map preference tiers green-yellow-red ([e44ba5c](https://github.com/aries1980/lodo/commit/e44ba5c8ffee4653a8337b3ee9b7793d91059d41))
- Read shared calendars as busy-only sources ([b821798](https://github.com/aries1980/lodo/commit/b82179807f56f7a61dac9bff1906113bf0ca75a4))
- Support task dependencies and nesting across planner, store, report and specs ([d9fc1b5](https://github.com/aries1980/lodo/commit/d9fc1b5da8eefa5d5fcdca85725705b6bec6166d))
- Store bindings in SQLite, reconcile calendar edits, write the schedule ([83892b9](https://github.com/aries1980/lodo/commit/83892b9092b483dd39c7beecf90fe6d631861e91))
- Read Google Calendar events and project busy time into forecast ([debba8d](https://github.com/aries1980/lodo/commit/debba8df93e5f38278db8302ac6ff657a109484c))
- Run the loopback OAuth flow and store the Google refresh token ([086d90c](https://github.com/aries1980/lodo/commit/086d90c0376fd02208d301a0c0ed4133b58c31b8))
- Wire the read side so check and forecast run for real ([11a3afa](https://github.com/aries1980/lodo/commit/11a3afa81282b1f9f9a0184258144500d624f96d))
- Resolve Logseq payloads into Tasks, Time Maps and a report ([f0f4c6a](https://github.com/aries1980/lodo/commit/f0f4c6ad374fb861f71ecb64d9d6886a852be0fb))
- Read a Logseq graph over its HTTP API with one file-graph adapter ([6ad717c](https://github.com/aries1980/lodo/commit/6ad717c4d931dc7e3572a0fba7c231381155c7d2))
- Load config.yaml and the 0600 credentials cache ([edc0277](https://github.com/aries1980/lodo/commit/edc02779496fc1ad63ba9efcf3b964878bac95aa))
- Add the I/O-free scheduling core (model, parse, grid, planner) ([47f9f27](https://github.com/aries1980/lodo/commit/47f9f2730d80fe07d229a4a9cf8e39399d1e1a2e))

### Bug fixes

- Fall back to patch on calendar 409 duplicate-id inserts ([d75f335](https://github.com/aries1980/lodo/commit/d75f335710986a870a5d36f6cce022a146a0b0bb))
- Fold Logseq camelCase keys back to kebab-case ([b80d228](https://github.com/aries1980/lodo/commit/b80d2287f3f2d594d2acc511f7e8076cb25123c3))
- Hint at logseq.insecure on TLS trust failure ([74ead07](https://github.com/aries1980/lodo/commit/74ead07fd4e73d4dbde682a97f07af5f1f9b124a))
- Trust the CI workspace in git so Hugo's GitInfo data builds ([101f38b](https://github.com/aries1980/lodo/commit/101f38b22b13da6aeb2032899c8a0b8048db78fb))

### Documentation

- Note the prompted Time Maps bootstrap across specs and guides ([6224f8a](https://github.com/aries1980/lodo/commit/6224f8a9206e9eddc97a3261776a00521e048bbd))
- Render secret-flags note as geekdoc warning hint ([cde0e0c](https://github.com/aries1980/lodo/commit/cde0e0cf12ea7b25873b28a298613200747045ca))
- Show multiple read_calendars in config example ([1a27c78](https://github.com/aries1980/lodo/commit/1a27c788926fc0153e718ffb4cdd5c6650fe7e17))
- Expand entering-tasks guide with front matter and nesting examples ([262a3b3](https://github.com/aries1980/lodo/commit/262a3b348b563c519a0592e2067756626b6acb80))
- Add shared-calendar busy-time and reconciliation diagrams ([63f6b01](https://github.com/aries1980/lodo/commit/63f6b01e24c2dcb4d076cff0baee823bd80c657b))
- Add scheduling eligibility, placement and infeasibility flowcharts ([b159016](https://github.com/aries1980/lodo/commit/b159016ef29c5f782500fc132c8c067a135d3a5c))
- Adding how to create your personal Google Calendar app ([b0877fd](https://github.com/aries1980/lodo/commit/b0877fd977fa92b35d7ff3afca2d807ab14ee248))
- Record Google Tasks API as out of scope ([335531c](https://github.com/aries1980/lodo/commit/335531c230c8dc65dd3c0de1cae5994a80b8e567))
- Keep a Pinned Block's span, not just its time ([7da16ca](https://github.com/aries1980/lodo/commit/7da16ca8c886be623515f4db9e9e36c165207484))
- Fix edit-on-github links for the docs site root ([73d5089](https://github.com/aries1980/lodo/commit/73d5089a6c7a05d753d8a8aea35c14c3dc64f69a))
- Make docs/ the self-contained Hugo site root ([756a127](https://github.com/aries1980/lodo/commit/756a127c06261a156c4b7b70e2a77fd62dc6b37e))
- Add ADR 0010 and clarify scheduling details ([1a01f1a](https://github.com/aries1980/lodo/commit/1a01f1a8a8d31db40b36883f2dd2fa3ce2ec54db))
- Add front matter and site preview instructions ([3b8031b](https://github.com/aries1980/lodo/commit/3b8031bbfb52d88430812e977e01f5bd197e14a0))
- Add v0.1 specification and ADRs ([f6ba8ee](https://github.com/aries1980/lodo/commit/f6ba8ee30858e0e3f5fd390aacee93ec347df840))


