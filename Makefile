GO       ?= go
BIN      := bin/rocketsled
UI_DIR   := web
GOLANGCI_VERSION := v2.13.2
# go.mod asks for a newer Go than the one on PATH may be; with GOTOOLCHAIN=auto
# the go command switches, but `-cover` builds shell out to whichever `go` is
# first on PATH. Putting the selected toolchain first keeps them the same.
export PATH := $(shell $(GO) env GOROOT)/bin:$(PATH)
GOLANGCI ?= $(shell test -x bin/golangci-lint && echo bin/golangci-lint || echo golangci-lint)

.PHONY: all build ui ui-dev run providers test lint lint-go lint-ui lint-tool fmt fmt-check check hooks generate tidy clean docker

all: ui build

build:
	$(GO) build -o $(BIN) ./cmd/rocketsled

ui:
	cd $(UI_DIR) && pnpm install --frozen-lockfile && pnpm build

ui-dev:
	cd $(UI_DIR) && pnpm install && pnpm dev

run: build providers
	$(BIN) serve -config rocketsled.yaml

providers:
	scripts/fetch-providers.sh data/plugin-cache

test:
	$(GO) test -race -cover ./...

lint: lint-go lint-ui

lint-go:
	$(GOLANGCI) run ./...

lint-ui:
	cd $(UI_DIR) && pnpm lint && pnpm typecheck

lint-tool:
	GOBIN=$(CURDIR)/bin $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

fmt:
	$(GOLANGCI) fmt ./...
	cd $(UI_DIR) && pnpm format

fmt-check:
	@test -z "$$($(GOLANGCI) fmt --diff ./... 2>&1)" || { $(GOLANGCI) fmt --diff ./...; echo "run make fmt"; exit 1; }
	cd $(UI_DIR) && pnpm format:check

# Everything CI checks, in the order that fails fastest.
check: fmt-check
	@mkdir -p bin && cp internal/domain/mocks/mocks.go bin/mocks.go.before
	$(GO) generate ./...
	@cmp -s bin/mocks.go.before internal/domain/mocks/mocks.go || { echo "the mocks were stale and are regenerated now; commit them"; exit 1; }
	$(GO) mod tidy -diff
	$(MAKE) lint test

hooks:
	git config core.hooksPath .githooks

generate:
	$(GO) generate ./...

tidy:
	$(GO) mod tidy

docker:
	docker build -t rocketsled:local .

clean:
	rm -rf bin $(UI_DIR)/dist/*
	touch $(UI_DIR)/dist/.gitkeep
