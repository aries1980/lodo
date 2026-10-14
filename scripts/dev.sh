#!/bin/sh
#
# Developer tasks for lodo, run in containers. Nothing but podman is needed on
# the host (plus curl or wget for the docs theme download) - no local Go, Hugo
# or make installation.
#
# Containers run as the invoking user: rootless podman maps the current uid
# into the container with --userns=keep-id, and --user overrides whatever
# USER the image declares. Files written by the tools - build output, module
# caches under /tmp, the fetched docs theme - are therefore owned by you.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

GO_IMAGE=${GO_IMAGE:-docker.io/library/golang:1.27.2}
LINT_IMAGE=${LINT_IMAGE:-docker.io/golangci/golangci-lint:v2.14.0-alpine}
HUGO_IMAGE=${HUGO_IMAGE:-ghcr.io/gohugoio/hugo:v0.168.0} # keep the tag in sync with .github/workflows/hugo.yml

# Pinned docs theme, fetched into docs/themes/ (gitignored) on demand.
GEEKDOC_VERSION=v4.1.4
GEEKDOC_SHA256=384675981b41bf70fbc2943d05fadb20ac6090f52c08550fd139842bdb8cb260
GEEKDOC_URL=https://github.com/thegeeklab/hugo-geekdoc/releases/download/$GEEKDOC_VERSION/hugo-geekdoc.tar.gz
THEME_DIR=$ROOT/docs/themes/hugo-geekdoc

die() {
	printf 'dev.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: scripts/dev.sh COMMAND

Go:
  build   build the CLI to dist/lodo
  test    run go test ./...
  vet     run go vet ./...
  fmt     format ./cmd and ./internal with gofmt
  lint    run golangci-lint
  tidy    run go mod tidy

Docs:
  docs        serve the site on http://localhost:1313/lodo/
  docs-build  build the site into docs/public/
  docs-theme  download the pinned geekdoc theme into docs/themes/
EOF
}

# podrun runs a container as the invoking user.
podrun() {
	command -v podman >/dev/null 2>&1 || die "podman is required"
	if [ -z "${podman_rootless:-}" ]; then
		podman_rootless=$(podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null || echo false)
	fi
	if [ "$podman_rootless" = true ]; then
		podman run --rm --userns=keep-id --user "$(id -u):$(id -g)" "$@"
	else
		podman run --rm --user "$(id -u):$(id -g)" "$@"
	fi
}

go_run() {
	podrun \
		-v "$ROOT:/app" -v /tmp:/tmp \
		-e GOCACHE=/tmp/lodo-go-cache \
		-e GOMODCACHE=/tmp/lodo-go-mod-cache \
		-w /app "$@"
}

hugo_run() {
	podrun \
		-v "$ROOT/docs:/project:Z" -v /tmp:/tmp \
		-e HUGO_CACHEDIR=/tmp/lodo-hugo-cache \
		-w /project "$@"
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "curl or wget is required to download the docs theme"
	fi
}

docs_theme() {
	if [ -f "$THEME_DIR/VERSION" ] && [ "$(cat "$THEME_DIR/VERSION")" = "$GEEKDOC_VERSION" ]; then
		printf 'geekdoc %s already in docs/themes/hugo-geekdoc\n' "$GEEKDOC_VERSION"
		return 0
	fi
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT HUP INT TERM
	printf 'downloading geekdoc %s...\n' "$GEEKDOC_VERSION"
	download "$GEEKDOC_URL" "$tmp/hugo-geekdoc.tar.gz"
	if ! echo "$GEEKDOC_SHA256  $tmp/hugo-geekdoc.tar.gz" | sha256sum -c - >/dev/null; then
		die "geekdoc $GEEKDOC_VERSION: checksum mismatch"
	fi
	mkdir "$tmp/theme"
	tar -xzf "$tmp/hugo-geekdoc.tar.gz" -C "$tmp/theme"
	rm -rf "$THEME_DIR"
	mkdir -p "$ROOT/docs/themes"
	mv "$tmp/theme" "$THEME_DIR"
	trap - EXIT HUP INT TERM
	rm -rf "$tmp"
	printf 'geekdoc %s ready in docs/themes/hugo-geekdoc\n' "$GEEKDOC_VERSION"
}

case ${1:-} in
build)
	mkdir -p "$ROOT/dist"
	go_run "$GO_IMAGE" go build -trimpath -o dist/lodo ./cmd/lodo
	;;
test)
	go_run "$GO_IMAGE" go test ./...
	;;
vet)
	go_run "$GO_IMAGE" go vet ./...
	;;
fmt)
	go_run "$GO_IMAGE" gofmt -l -w ./cmd ./internal
	;;
lint)
	go_run "$LINT_IMAGE" golangci-lint run --modules-download-mode=mod --timeout=5m
	;;
tidy)
	go_run "$GO_IMAGE" go mod tidy
	;;
docs-theme)
	docs_theme
	;;
docs)
	docs_theme
	hugo_run -p 1313:1313 "$HUGO_IMAGE" server --bind 0.0.0.0
	;;
docs-build)
	docs_theme
	hugo_run "$HUGO_IMAGE" --minify
	;;
'' | help | --help | -h)
	usage
	;;
*)
	die "unknown command: $1 (try --help)"
	;;
esac
