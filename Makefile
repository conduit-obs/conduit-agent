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
        helm-lint helm-package helm-push helm-sign helm-publish

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
