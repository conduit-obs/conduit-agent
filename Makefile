SHELL := /bin/bash

# Conduit version surface. M1 has no ldflag injection (subcommands are stubs);
# this variable exists so M2+ can plug it into Go's -X linker flags.
VERSION ?= 0.0.0-dev
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# Pinned OpenTelemetry Collector Builder (OCB) version. Drives the upstream
# OTel collector + contrib MINOR that ships in Conduit (see docs/adr/adr-0014).
# Bump this in lockstep with the entries in builder-config.yaml.
OCB_VERSION ?= 0.151.0

GO ?= go
BIN_DIR := bin
DIST_DIR := dist
BUILD_DIR := build
OCB_OUTPUT_DIR := $(BUILD_DIR)/collector
BUILDER_CONFIG := builder-config.yaml
COLLECTOR_DIR := internal/collector

# Files OCB generates that we keep verbatim (modulo the package rewrite).
# main.go is dropped because internal/collector/collector.go replaces its
# func main() with the exported DefaultSettings/Run pair, going straight
# through otelcol.NewCollector instead of the embedded cobra command. go.mod
# and go.sum are dropped because the root module owns dependencies (single-
# module scheme). main_others.go and main_windows.go are excluded from M2.C;
# Windows-service handling lands when packaging does (M4).
OCB_KEEP_FILES := components.go

BIN := $(BIN_DIR)/conduit

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

# Windows OCB asset has an .exe suffix; everything else is plain.
ifeq ($(GOOS),windows)
OCB_BIN := $(BIN_DIR)/ocb.exe
OCB_ASSET := ocb_$(OCB_VERSION)_$(GOOS)_$(GOARCH).exe
else
OCB_BIN := $(BIN_DIR)/ocb
OCB_ASSET := ocb_$(OCB_VERSION)_$(GOOS)_$(GOARCH)
endif
OCB_URL := https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/cmd%2Fbuilder%2Fv$(OCB_VERSION)/$(OCB_ASSET)

.PHONY: help build test vendor lint fmt install-ocb build-ocb release-snapshot clean

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the conduit binary into ./bin/conduit (M1: stubs only, no embedded collector)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./

test: ## Run all unit tests
	$(GO) test ./...

vendor: ## Resolve module dependencies (M1: no `go mod vendor` until M2 forces it)
	$(GO) mod tidy

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format Go sources
	$(GO) fmt ./...

install-ocb: $(OCB_BIN) ## Download the pinned OpenTelemetry Collector Builder into ./bin

$(OCB_BIN):
	@mkdir -p $(BIN_DIR)
	@echo "Downloading OCB v$(OCB_VERSION) for $(GOOS)/$(GOARCH)..."
	@curl -fsSL -o $(OCB_BIN) "$(OCB_URL)"
	@chmod +x $(OCB_BIN)
	@echo "OCB installed at $(OCB_BIN)."

build-ocb: $(OCB_BIN) $(BUILDER_CONFIG) ## Generate the embedded collector source from builder-config.yaml and fold it into internal/collector/
	@echo "Generating OCB output into $(OCB_OUTPUT_DIR)..."
	@rm -rf $(OCB_OUTPUT_DIR)
	@mkdir -p $(OCB_OUTPUT_DIR)
	@$(OCB_BIN) --config=$(BUILDER_CONFIG) --skip-compilation
	@echo "Folding OCB output into $(COLLECTOR_DIR) (kept files: $(OCB_KEEP_FILES))..."
	@find $(COLLECTOR_DIR) -maxdepth 1 -name '*.go' ! -name 'collector.go' -delete
	@for f in $(OCB_KEEP_FILES); do \
		sed -e 's/^package main$$/package collector/' \
			"$(OCB_OUTPUT_DIR)/$$f" > "$(COLLECTOR_DIR)/$$f"; \
	done
	@$(GO) mod tidy
	@echo "OCB output folded into $(COLLECTOR_DIR). Run 'make build' to verify."

release-snapshot: ## Run goreleaser in snapshot mode (no publish, no signing; M1 only produces tarballs)
	goreleaser release --snapshot --clean --skip=publish

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(BUILD_DIR)
