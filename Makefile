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

.PHONY: help build test vendor lint fmt install-ocb build-ocb release-snapshot clean \
        kind-up kind-image kind-load kind-deploy kind-test kind-down kind-smoketest \
        helm-lint helm-package helm-push helm-sign helm-publish \
        obi-vendor obi-clean obi-image obi-kind-load obi-kind-deploy \
        update-goldens vulncheck

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the conduit binary into ./bin/conduit. On Linux, requires `make obi-vendor` first (OBI is compiled in unconditionally per ADR-0020 sub-decision 1).
	@mkdir -p $(BIN_DIR)
	@if [ "$(GOOS)" = "linux" ] && { [ ! -d "third_party/obi/pkg" ] || [ ! -f "go.work" ]; }; then \
		echo "make build: this is a Linux build but the OBI workspace isn't set up."; \
		echo "Linux conduit binaries link OBI unconditionally (ADR-0020 sub-decision 1)."; \
		echo "Run 'make obi-vendor' once to clone OBI, generate its eBPF Go bindings,"; \
		echo "and write the gitignored go.work that points the build at the local checkout."; \
		echo "Then re-run 'make build'. macOS / Windows builds skip OBI via //go:build tags"; \
		echo "and do not need this step."; \
		exit 1; \
	fi
	$(GO) build -o $(BIN) ./

test: ## Run all unit tests
	$(GO) test ./...

update-goldens: ## Rewrite internal/expander/testdata/goldens/<case>/expected.yaml from the current renderer (M12.B)
	$(GO) test ./internal/expander -run TestExpand_Goldens -update

vulncheck: ## Run govulncheck against the module — release-blocker per [07-testing-and-conformance-plan.md] §M12 release gates
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

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
	@# OCB's generated import ordering doesn't match gofmt's grouping
	@# rules, which trips golangci-lint's gofmt linter in CI. Run gofmt
	@# here so the folded output matches the lint contract; no behavior
	@# change, just a stable canonical layout.
	@gofmt -w $(COLLECTOR_DIR)
	@$(GO) mod tidy
	@echo "OCB output folded into $(COLLECTOR_DIR). Run 'make build' to verify."

release-snapshot: ## Run goreleaser in snapshot mode (no publish, no signing; M1 only produces tarballs)
	goreleaser release --snapshot --clean --skip=publish

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST_DIR) $(BUILD_DIR)

# ----------------------------------------------------------------------------
# kind smoke recipe (M5.C). Bring up a local Kubernetes cluster, build the
# conduit image, install the chart, send a test trace, and verify it lands.
# Usage:
#   make kind-smoketest         # full sequence (~3 min on first run)
#   make kind-up kind-image kind-load kind-deploy kind-test  # step-by-step
#   make kind-down              # tear down the cluster
#
# kind, helm, docker, and kubectl must be on PATH; the recipe runs in a
# disposable cluster named "conduit-smoke" so it never touches the
# operator's other contexts.
# ----------------------------------------------------------------------------
KIND_CLUSTER ?= conduit-smoke
KIND_IMAGE ?= conduit:kind
KIND_NAMESPACE ?= conduit
KIND_RELEASE ?= conduit
# Honeycomb rejects the dummy key but the agent still runs and the debug
# exporter still logs every signal — that's how kind-test verifies things
# end-to-end without a real Honeycomb tenant.
KIND_API_KEY ?= hcaik_smoke_test_DUMMY

kind-up: ## Create the kind cluster (idempotent)
	@kind get clusters 2>/dev/null | grep -q "^$(KIND_CLUSTER)$$" \
		|| kind create cluster --name $(KIND_CLUSTER) --wait 60s

kind-image: ## Build the conduit:kind image from the source Dockerfile
	docker build -t $(KIND_IMAGE) -f deploy/docker/Dockerfile .

kind-load: ## Load the conduit:kind image into the kind cluster
	kind load docker-image $(KIND_IMAGE) --name $(KIND_CLUSTER)

kind-deploy: ## Helm-install the chart into the cluster
	kubectl --context kind-$(KIND_CLUSTER) create namespace $(KIND_NAMESPACE) \
		--dry-run=client -o yaml | kubectl --context kind-$(KIND_CLUSTER) apply -f -
	helm --kube-context kind-$(KIND_CLUSTER) upgrade --install $(KIND_RELEASE) \
		deploy/helm/conduit-agent \
		--namespace $(KIND_NAMESPACE) \
		--set conduit.serviceName=kind-smoke \
		--set conduit.deploymentEnvironment=kind \
		--set image.repository=conduit \
		--set image.tag=kind \
		--set image.pullPolicy=Never \
		--set honeycomb.apiKey=$(KIND_API_KEY) \
		--wait --timeout 120s

kind-test: ## Send a test trace and verify the agent received it
	@echo "Waiting for the conduit DaemonSet to roll out..."
	kubectl --context kind-$(KIND_CLUSTER) -n $(KIND_NAMESPACE) rollout status \
		ds/$(KIND_RELEASE)-conduit-agent --timeout=120s
	@echo "Sending a test trace via the Service..."
	kubectl --context kind-$(KIND_CLUSTER) -n $(KIND_NAMESPACE) run smoketest-curl \
		--image=curlimages/curl:8.10.1 --restart=Never --rm -it -- \
		curl -sS -X POST http://$(KIND_RELEASE)-conduit-agent:4318/v1/traces \
			-H 'Content-Type: application/json' \
			-d '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"smoketest"}}]},"scopeSpans":[{"spans":[{"traceId":"01020304050607080102030405060708","spanId":"0102030405060708","name":"kind-smoke","startTimeUnixNano":1,"endTimeUnixNano":2}]}]}]}'
	@echo
	@echo "Checking conduit's debug exporter logs for the trace..."
	@kubectl --context kind-$(KIND_CLUSTER) -n $(KIND_NAMESPACE) logs \
		-l app.kubernetes.io/name=conduit-agent --tail=200 \
		| grep -E "(TracesExporter|kind-smoke|smoketest)" \
		|| { echo "kind-test: agent did not log the trace; full pod logs:"; \
		     kubectl --context kind-$(KIND_CLUSTER) -n $(KIND_NAMESPACE) logs \
		       -l app.kubernetes.io/name=conduit-agent --tail=200; exit 1; }
	@echo "kind-test: ok"

kind-down: ## Delete the kind cluster
	kind delete cluster --name $(KIND_CLUSTER)

kind-smoketest: kind-up kind-image kind-load kind-deploy kind-test ## Full kind smoke sequence (excluding kind-down)
	@echo "kind smoke complete. Run 'make kind-down' to tear down."

# ----------------------------------------------------------------------------
# OBI build integration (ADR-0020 sub-decision 1: "single Linux binary, OBI
# compiled in unconditionally"). The on-Linux build path is now driven by
# build tags, not by regenerating internal/collector/components.go:
#
#   * internal/collector/components_obi_linux.go (//go:build linux) imports
#     go.opentelemetry.io/obi/collector and registers the receiver factory.
#   * internal/collector/components_obi_other.go (//go:build !linux) is a
#     no-op stub so macOS / Windows compile without resolving the OBI module.
#   * components.go (OCB-generated, base builder-config.yaml, no OBI imports)
#     calls addPlatformReceivers() which the tagged file fills in.
#
# That leaves one external constraint: upstream OBI v0.8.0 doesn't ship the
# pre-generated eBPF Go bindings (see ADR-0020 "Open question: build
# pipeline"), so a real Linux build needs them generated locally. `make
# obi-vendor` clones third_party/obi/ from upstream, runs upstream's `make
# docker-generate` to produce the bindings, and writes a gitignored go.work
# pointing the toolchain at the local checkout.
#
# Why a Go workspace (go.work) instead of `go mod edit -replace`: goreleaser
# refuses to build from a dirty tree, and editing go.mod / running `go mod
# tidy` at vendor time leaves both files modified relative to the tagged
# commit. Workspaces resolve the OBI require against ./third_party/obi
# without touching go.mod or go.sum, so `git status` stays clean post-vendor
# and goreleaser's "previous=v… current=v…" gate is satisfied. Workspaces
# are the Go-blessed mechanism for exactly this case (since 1.18).
#
# Typical local flow on Linux:
#   make obi-vendor    # one-time per machine: clone OBI + codegen bindings + write go.work
#   make build         # produces ./bin/conduit with OBI linked
#   make obi-image     # produces conduit:obi container (cross-compiles on host)
#
# CI .github/workflows/ci.yml runs `make obi-vendor` on every Linux job
# before any Go toolchain step. macOS / Windows jobs skip it; the build tag
# excludes the OBI imports there so the upstream proxy v0.8.0 (which lacks
# the BPF bindings) suffices for module resolution without ever being
# compiled.
# ----------------------------------------------------------------------------
OBI_VERSION ?= v0.8.0
OBI_REPO ?= https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation.git
OBI_DIR := third_party/obi
OBI_IMAGE ?= conduit:obi
# Helm release name is reused from the kind smoke variables above so
# obi-kind-deploy lands the chart in the same conduit namespace as the
# default smoke flow expects.
OBI_KIND_RELEASE ?= $(KIND_RELEASE)

obi-vendor: ## Clone go.opentelemetry.io/obi into third_party/obi/, generate its eBPF Go bindings, and write a gitignored go.work that uses the local checkout
	@if [ -d "$(OBI_DIR)/.git" ]; then \
		echo "OBI checkout already at $(OBI_DIR); skipping clone. Delete the directory and re-run to refresh."; \
	else \
		echo "Cloning OBI $(OBI_VERSION) into $(OBI_DIR)..."; \
		mkdir -p third_party; \
		git clone --depth 1 --branch $(OBI_VERSION) $(OBI_REPO) $(OBI_DIR); \
	fi
	@echo "Generating eBPF Go bindings in $(OBI_DIR) (Docker required; ~2 min first run)..."
	@command -v docker >/dev/null 2>&1 || { echo "obi-vendor: docker not found on PATH; OBI's make docker-generate requires it"; exit 1; }
	@$(MAKE) -C $(OBI_DIR) docker-generate
	@# Write a Go workspace file so `go build`/`go test` resolve the OBI
	@# `require` line in the committed go.mod against the local checkout
	@# instead of the upstream proxy. This keeps the committed go.mod /
	@# go.sum bit-identical to HEAD — goreleaser's "git is dirty" gate
	@# rejected the previous approach (mutating go.mod with `go mod edit
	@# -replace`) on every release tag. go.work is gitignored, so the
	@# workspace only ever lives in working trees + CI runners.
	@echo "Writing go.work to use ./$(OBI_DIR) (committed go.mod / go.sum left untouched)..."
	@printf 'go 1.25.9\n\nuse (\n\t.\n\t./$(OBI_DIR)\n)\n' > go.work
	@echo "OBI vendored at $(OBI_DIR); go.work points the build at it. 'make build' on Linux now links OBI in."

obi-clean: ## Remove go.work + go.work.sum so the toolchain falls back to the committed go.mod (idempotent)
	@echo "Removing go.work and go.work.sum (committed go.mod is the source of truth without them)..."
	@rm -f go.work go.work.sum
	@echo "Workspace cleared. third_party/obi/ left in place; rm -rf to discard."

OBI_IMAGE_STAGE := $(DIST_DIR)/conduit-obi-image
# Default to the host architecture for the kind-target build. kind nodes on
# macOS Apple Silicon run as arm64 (kindest/node images are multi-arch);
# operators on Intel Macs naturally land on amd64. Override by setting
# OBI_IMAGE_GOARCH=amd64 (or arm64) at the make command line.
OBI_IMAGE_GOARCH ?= $(GOARCH)

obi-image: ## Cross-compile a Linux conduit binary with OBI linked and bake it into conduit:obi (host build, sidesteps Colima's linker memory ceiling)
	@if [ ! -d "$(OBI_DIR)/pkg" ]; then \
		echo "obi-image: $(OBI_DIR) is missing or empty; run 'make obi-vendor' first."; \
		exit 1; \
	fi
	@echo "Cross-compiling conduit for linux/$(OBI_IMAGE_GOARCH) with OBI linked..."
	@rm -rf $(OBI_IMAGE_STAGE)
	@mkdir -p $(OBI_IMAGE_STAGE)
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(OBI_IMAGE_GOARCH) \
		$(GO) build -trimpath -ldflags="-s -w" -o $(OBI_IMAGE_STAGE)/conduit ./
	@cp deploy/docker/conduit.yaml.default $(OBI_IMAGE_STAGE)/conduit.yaml.default
	@cp deploy/docker/Dockerfile.obi $(OBI_IMAGE_STAGE)/Dockerfile
	@echo "Building $(OBI_IMAGE) from $(OBI_IMAGE_STAGE) (~50 MB context, ~5 s)..."
	docker build --platform linux/$(OBI_IMAGE_GOARCH) -t $(OBI_IMAGE) $(OBI_IMAGE_STAGE)
	@echo "Built $(OBI_IMAGE). Run 'make obi-kind-load' to push it into the kind cluster."

obi-kind-load: ## Load the conduit:obi image into the kind cluster (assumes 'make kind-up' already ran)
	kind load docker-image $(OBI_IMAGE) --name $(KIND_CLUSTER)

obi-kind-deploy: ## Helm-install (or upgrade) the chart into the kind cluster with obi.enabled=true
	kubectl --context kind-$(KIND_CLUSTER) create namespace $(KIND_NAMESPACE) \
		--dry-run=client -o yaml | kubectl --context kind-$(KIND_CLUSTER) apply -f -
	helm --kube-context kind-$(KIND_CLUSTER) upgrade --install $(OBI_KIND_RELEASE) \
		deploy/helm/conduit-agent \
		--namespace $(KIND_NAMESPACE) \
		-f tools/local-k8s/values-obi.yaml \
		$(if $(HONEYCOMB_API_KEY),--set honeycomb.apiKey=$(HONEYCOMB_API_KEY),) \
		--wait --timeout 180s

# ----------------------------------------------------------------------------
# Helm chart packaging + OCI publishing (M5.D). The chart lives at
# deploy/helm/conduit-agent; the published artifact is
# oci://ghcr.io/conduit-obs/charts/conduit-agent per ADR-0019.
#
# Typical flows:
#   make helm-lint       # local sanity check (helm lint + helm template)
#   make helm-package    # produce dist/conduit-agent-<version>.tgz
#   make helm-publish    # package + push + sign (CI flow)
#
# Authentication: helm push to ghcr.io needs `helm registry login` first.
# In CI that's `helm registry login ghcr.io -u $GITHUB_ACTOR -p $GITHUB_TOKEN`;
# locally use a PAT with write:packages scope.
#
# Signing: cosign keyless OIDC in CI (no key material; the workflow
# inherits id-token: write); locally users can override COSIGN_KEY to
# point at a private key file. Both produce a transparency-log entry
# discoverable via `cosign verify`.
# ----------------------------------------------------------------------------
HELM ?= helm
HELM_CHART_DIR := deploy/helm/conduit-agent
HELM_CHART_VERSION := $(shell awk '/^version:/ {print $$2; exit}' $(HELM_CHART_DIR)/Chart.yaml)
HELM_CHART_PACKAGE := $(DIST_DIR)/conduit-agent-$(HELM_CHART_VERSION).tgz
HELM_OCI_REPO ?= oci://ghcr.io/conduit-obs/charts
COSIGN ?= cosign
COSIGN_KEY ?=

helm-lint: ## Run helm lint + a default-values render to catch template regressions
	$(HELM) lint $(HELM_CHART_DIR) \
		--set conduit.serviceName=lint-smoke \
		--set honeycomb.apiKey=lint_dummy
	@$(HELM) template lint-smoke $(HELM_CHART_DIR) \
		--set conduit.serviceName=lint-smoke \
		--set honeycomb.apiKey=lint_dummy >/dev/null

helm-package: $(HELM_CHART_PACKAGE) ## Package the chart into dist/conduit-agent-<version>.tgz

$(HELM_CHART_PACKAGE):
	@mkdir -p $(DIST_DIR)
	$(HELM) package $(HELM_CHART_DIR) --destination $(DIST_DIR)
	@echo "Packaged $(HELM_CHART_PACKAGE)."

helm-push: helm-package ## Push the packaged chart to the OCI registry (requires `helm registry login`)
	$(HELM) push $(HELM_CHART_PACKAGE) $(HELM_OCI_REPO)

helm-sign: helm-package ## Sign the packaged chart with cosign (keyless OIDC by default; set COSIGN_KEY=<path> for key-based)
	@if [ -n "$(COSIGN_KEY)" ]; then \
		echo "Signing $(HELM_CHART_PACKAGE) with key $(COSIGN_KEY)..."; \
		$(COSIGN) sign-blob --yes --key "$(COSIGN_KEY)" \
			--output-signature $(HELM_CHART_PACKAGE).sig \
			--output-certificate $(HELM_CHART_PACKAGE).pem \
			$(HELM_CHART_PACKAGE); \
	else \
		echo "Signing $(HELM_CHART_PACKAGE) with cosign keyless OIDC..."; \
		$(COSIGN) sign-blob --yes \
			--output-signature $(HELM_CHART_PACKAGE).sig \
			--output-certificate $(HELM_CHART_PACKAGE).pem \
			$(HELM_CHART_PACKAGE); \
	fi
	@echo "Signature: $(HELM_CHART_PACKAGE).sig"
	@echo "Certificate: $(HELM_CHART_PACKAGE).pem"

helm-publish: helm-lint helm-push helm-sign ## Full publish flow: lint + package + push + sign
	@echo "Published $(HELM_CHART_PACKAGE) to $(HELM_OCI_REPO)/conduit-agent:$(HELM_CHART_VERSION)."
	@echo "Verify with:"
	@echo "  helm pull $(HELM_OCI_REPO)/conduit-agent --version $(HELM_CHART_VERSION)"
	@echo "  cosign verify-blob \\"
	@echo "    --certificate-identity-regexp 'https://github.com/conduit-obs/.*' \\"
	@echo "    --certificate-oidc-issuer https://token.actions.githubusercontent.com \\"
	@echo "    --signature $(HELM_CHART_PACKAGE).sig \\"
	@echo "    --certificate $(HELM_CHART_PACKAGE).pem \\"
	@echo "    $(HELM_CHART_PACKAGE)"
