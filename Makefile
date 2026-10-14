GO_IMAGE   ?= golang:1.27.2
LINT_IMAGE ?= docker.io/golangci/golangci-lint:v2.14.0-alpine
PODMAN     ?= podman

PODMAN_RUN ?= $(PODMAN) run --rm -v "$(CURDIR):/app" -v /tmp:/tmp \
  -e GOCACHE=/tmp/lodo-go-cache -e GOMODCACHE=/tmp/lodo-go-mod-cache -w /app

.PHONY: build test vet fmt lint tidy

build:
	$(PODMAN_RUN) $(GO_IMAGE) go build -trimpath -o /tmp/lodo ./cmd/lodo

test:
	$(PODMAN_RUN) $(GO_IMAGE) go test ./...

vet:
	$(PODMAN_RUN) $(GO_IMAGE) go vet ./...

fmt:
	$(PODMAN_RUN) $(GO_IMAGE) gofmt -l -w ./cmd ./internal

lint:
	$(PODMAN_RUN) $(LINT_IMAGE) golangci-lint run --modules-download-mode=mod --timeout=5m

tidy:
	$(PODMAN_RUN) $(GO_IMAGE) go mod tidy
