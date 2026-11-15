---
title: "Themes"
weight: 95
---

> Status: **draft (v0.1).**

Human output (CLI text and the TUI) shares one named color scheme from
`ui.theme` in `config.yaml`. The default is `concrete`: plain text, no ANSI
colors.

Shipped schemes (embedded in the binary, also installed to
`/usr/share/lodo/themes/` by the deb/rpm/apk packages):

| Name | Source |
| --- | --- |
| `monokai` | Monokai |
| `tokyonight` | TokyoNight Night (`folke/tokyonight.nvim`) |
| `solarized-dark` | Solarized dark (Ethan Schoonover) |
| `solarized-light` | Solarized light (Ethan Schoonover) |
| `synthwave` | SynthWave '84 (neon on dark purple) |
| `gruvbox` | Gruvbox dark |
| `dracula` | Dracula |
| `nord` | Nord |
| `catppuccin-mocha` | Catppuccin Mocha |

## Theme files

A theme is one YAML file with `cli` (ANSI roles) and `tui` (lipgloss roles)
sections. Colors are hex (`#RRGGBB`) or 0-255 palette numbers; empty means
unstyled for that role. TUI bold/italic/underline stay fixed; only colors are
themed.

```yaml
name: monokai
cli:
  bold: "1"
  dim: "2"
  red: "#F92672"
  green: "#A6E22E"
  yellow: "#E6DB74"
  cyan: "#66D9EF"
tui:
  propKey: "#66D9EF"
  propSep: "#75715E"
  todo: "#E6DB74"
  doing: "#A6E22E"
  done: "#75715E"
  code: "#AE81FF"
  link: "#66D9EF"
  ref: "#AE81FF"
  head: "#F8F8F2"
  hint: "#75715E"
  title: "#F8F8F2"
  dim: "#75715E"
  err: "#F92672"
  ok: "#A6E22E"
  select: "#E6DB74"
  tableBorder: "#75715E"
  tableSelFg: "#F8F8F2"
  tableSelBg: "#49483E"
```

`ui.theme` accepts a shipped name (`concrete` plus the table above) or a file
path (absolute, `~`-relative, or relative to the config file). Name lookup order:

1. shipped schemes compiled into the binary (`concrete` plus the table above),
2. `$XDG_CONFIG_HOME/lodo/themes/<name>.yaml` (usually
   `~/.config/lodo/themes/<name>.yaml`),
3. `/usr/share/lodo/themes/<name>.yaml` for system-wide installs.

All shipped schemes are embedded in the binary (`//go:embed themes/*.yaml`),
so they work on every platform with no extra files. The release tarballs/zips
carry a `themes/` folder with the same YAML sources for reference, and the
deb/rpm/apk packages additionally install them to `/usr/share/lodo/themes/`.
Per-user overrides live in `~/.config/lodo/themes/`.

Shipped sources live in `internal/theme/themes/`; copy them to
`/usr/share/lodo/themes/` to install system-wide. Unknown names, missing
files, and invalid files warn on stderr and fall back to `concrete` — a bad
theme never fails a run.
