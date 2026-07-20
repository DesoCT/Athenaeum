# Athenaeum build and development commands (spec 02 section 8).
#
# The release artifact is a single Go executable with the compiled Svelte
# frontend embedded, and it must build with CGO disabled so cross-compilation
# stays trivial (constitution C6, requirement N4).

SHELL := /bin/bash
.DEFAULT_GOAL := help

VERSION ?= 0.1.0-dev
BIN_DIR := bin
BINARY := $(BIN_DIR)/athenaeum
LDFLAGS := -s -w -X main.version=$(VERSION)

GO ?= go
NPM ?= npm

export CGO_ENABLED := 0

.PHONY: help
help: ## Show available commands
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: deps
deps: ## Install frontend dependencies
	cd web && $(NPM) ci || (cd web && $(NPM) install)

.PHONY: web
web: ## Type-check and build the frontend into web/dist
	cd web && $(NPM) run build

.PHONY: build
build: web ## Build the release executable with the frontend embedded
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/athenaeum
	@echo "built $(BINARY) ($(VERSION))"

.PHONY: test
test: test-go test-web ## Run all tests

# The release build sets CGO_ENABLED=0 so a single static binary cross-compiles
# cleanly. The race detector needs cgo, so the race run re-enables it for tests
# only, and falls back to a non-race run when no C toolchain is present.
.PHONY: test-go
test-go: ## Run Go unit and integration tests, with the race detector when possible
	@if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1; then \
		echo "go test -race (cgo enabled for tests only)"; \
		CGO_ENABLED=1 $(GO) test ./... -race -count=1; \
	else \
		echo "no C toolchain found; running without the race detector"; \
		$(GO) test ./... -count=1; \
	fi

.PHONY: test-web
test-web: ## Type-check and unit-test the frontend
	cd web && $(NPM) run check
	cd web && $(NPM) test

.PHONY: test-acceptance
test-acceptance: build ## Run acceptance tests against the release binary
	ATHENAEUM_BINARY=$(CURDIR)/$(BINARY) $(GO) test ./test/acceptance/... -count=1 -v

.PHONY: lint
lint: ## Vet Go sources and check formatting
	$(GO) vet ./...
	@unformatted=$$(gofmt -l . | grep -v '^web/node_modules' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: dev
dev: ## Run the Go API and the Vite dev server together
	@echo "Go API on :7777, Vite on :5173 — open http://localhost:5173 after bootstrapping"
	ATHENAEUM_DEV_ORIGIN=http://localhost:5173 \
		$(GO) run ./cmd/athenaeum serve examples/athenaeum.toml --no-open & \
	cd web && $(NPM) run dev; \
	kill %1 2>/dev/null || true

.PHONY: package
package: web ## Cross-compile release archives for macOS and Linux
	@mkdir -p dist
	@for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "packaging $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/athenaeum-$$os-$$arch ./cmd/athenaeum || exit 1; \
		tar -czf dist/athenaeum-$(VERSION)-$$os-$$arch.tar.gz \
			-C dist athenaeum-$$os-$$arch -C $(CURDIR) LICENSE README.md || exit 1; \
		rm -f dist/athenaeum-$$os-$$arch; \
	done
	@echo "archives in dist/"

.PHONY: clean
clean: ## Remove build outputs
	rm -rf $(BIN_DIR) dist web/dist
	@mkdir -p web/dist && touch web/dist/.gitkeep
