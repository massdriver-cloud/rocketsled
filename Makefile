GO       ?= go
BIN      := bin/rocketsled
UI_DIR   := web
# A golangci-lint built with an older Go than go.mod targets refuses to run;
# `make lint-tool` builds one with the current toolchain into bin/.
GOLANGCI ?= $(shell test -x bin/golangci-lint && echo bin/golangci-lint || echo golangci-lint)

.PHONY: all build ui ui-dev run providers test lint lint-tool fmt generate tidy clean docker

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

lint:
	$(GOLANGCI) run ./...
	cd $(UI_DIR) && pnpm lint && pnpm typecheck

lint-tool:
	GOBIN=$(CURDIR)/bin $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

fmt:
	$(GOLANGCI) fmt ./...

generate:
	$(GO) generate ./...

tidy:
	$(GO) mod tidy

docker:
	docker build -t rocketsled:local .

clean:
	rm -rf bin $(UI_DIR)/dist/*
	touch $(UI_DIR)/dist/.gitkeep
