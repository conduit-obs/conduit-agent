# syntax=docker/dockerfile:1.7
#
# Conduit V0 Docker image.
#
# This Dockerfile is self-contained: `docker build .` from the repo root
# produces a usable image without any goreleaser plumbing. The release
# pipeline (M4.C) will use this same Dockerfile but feed it pre-built
# binaries via goreleaser's docker integration so each arch image is
# built from the artifact goreleaser already shipped to the GitHub
# release — keeping the source-build path and the release-build path on
# the same Dockerfile so they don't drift.
#
# Acceptance constraints baked into this image:
#   - Image runs as non-root by default (USER nonroot below).
#   - Health check endpoint reachable (health_check extension is wired
#     into every Conduit pipeline; bind to 0.0.0.0:13133 — see
#     internal/expander/templates/base.yaml.tmpl).
#   - OTLP receivers bind to 0.0.0.0 in the Docker profile (the in-image
#     default conduit.yaml below sets profile.mode=docker, which makes
#     internal/expander emit 0.0.0.0:4317 / :4318 instead of the host
#     default of 127.0.0.1).
#   - Multi-arch (amd64 + arm64) — handled at release time by goreleaser
#     buildx (M4.C); local `docker build` follows the host arch.

# ----------------------------------------------------------------------------
# Stage 1: builder
# ----------------------------------------------------------------------------
# Pinned to the Go MAJOR.MINOR declared in go.mod. bookworm rather than alpine
# so we get glibc-free static builds without alpine's musl quirks (and the
# bookworm image has CA certs pre-installed for module fetches).
FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH

# CGO_ENABLED=0 produces a fully static binary so the runtime stage can be
# distroless:static. -trimpath + -s -w drop debug info and absolute paths so
# the image is smaller and reproducible across hosts.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/conduit ./

# ----------------------------------------------------------------------------
# Stage 2: runtime
# ----------------------------------------------------------------------------
# distroless:static gives us no shell, no package manager, and runs as a
# nonroot user (UID 65532) by default. Smallest sane base for a static Go
# binary that still ships ca-certificates (needed for HTTPS egress to
# Honeycomb).
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Default in-image conduit.yaml — sets profile.mode=docker so the expander
# emits OTLP receivers bound to 0.0.0.0 (peer containers can reach them).
# Override at run time by mounting your own at /etc/conduit/conduit.yaml.
COPY --chown=65532:65532 deploy/docker/conduit.yaml.default /etc/conduit/conduit.yaml

COPY --from=builder --chown=65532:65532 /out/conduit /usr/bin/conduit

# 4317: OTLP/gRPC, 4318: OTLP/HTTP, 13133: health_check (always on; see
# internal/expander/templates/base.yaml.tmpl).
EXPOSE 4317 4318 13133

USER nonroot:nonroot

# Liveness/readiness probing in compose / k8s should HTTP-GET :13133, which
# returns 200 when every pipeline is healthy. We deliberately do not embed a
# Dockerfile HEALTHCHECK directive: distroless has no shell, and pulling in
# wget/curl/grpc_health_probe just to satisfy HEALTHCHECK is more attack
# surface than the orchestrator-level probe it would replace.

ENTRYPOINT ["/usr/bin/conduit"]
CMD ["run", "--config", "/etc/conduit/conduit.yaml"]
