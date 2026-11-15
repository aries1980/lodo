# Translating Lodo

Human-readable UI text and `--help` live in PO files here. The base
language is `en-US` (`en-us.po`); the first target is `en-GB`
(`en-gb.po`). `--json` output, logs, and dates stay machine-stable
and are never translated. `.po` files are read directly at startup,
so there is no compile step; `msgfmt --check` only validates syntax.

To add a language, copy `en-us.po`, set the `Language:` header, fill
in `msgstr` entries, and run `./scripts/dev.sh i18n-lint`.

## Tools

- [Poedit](https://poedit.net) — desktop PO editor (Linux, macOS, Windows).
- [Weblate](https://weblate.org) — hosted continuous localization.
- [Transifex](https://www.transifex.com) — hosted localization platform.
- [Lokalise](https://lokalise.com) — hosted localization platform.
- [GNU gettext](https://www.gnu.org/software/gettext/) — `msgmerge` /
  `msgfmt` CLI validation; [Alpine package](https://pkgs.alpinelinux.org/package/edge/main/x86/gettext)
  provides `msgfmt`.
