# deploy/docker

Docker deployment artifacts for Conduit (M4). Multi-arch image build
(both the source-build path and the goreleaser release path), in-image
default config, and a runnable compose example. Image signing via cosign
and SBOM attestations land in M12 (gated on OQ-5 in the decision log).

## Files

| Path | Purpose |
|---|---|
| [`Dockerfile`](Dockerfile) | Self-contained multi-stage build. Stage 1 compiles a static Go binary from the repo; stage 2 ships it on `gcr.io/distroless/static-debian12:nonroot`. This is the dev / contributor path: a fresh `git clone && docker build .` produces a usable image without goreleaser. |
| [`Dockerfile.goreleaser`](Dockerfile.goreleaser) | Release-pipeline variant fed by goreleaser's `dockers:` blocks. Skips the Go toolchain because goreleaser already produced the static binary; copies it from the per-arch build context. |
| [`conduit.yaml.default`](conduit.yaml.default) | Default in-image `/etc/conduit/conduit.yaml`. Sets `profile.mode: docker` so the expander binds OTLP receivers to `0.0.0.0`. Loaded by both Dockerfiles. |
| [`compose-linux-host.yaml`](compose-linux-host.yaml) | Runnable compose example: a single `conduit` service, ports published to the host loopback. |

## Quick start

Build the image locally, then run it:

```bash
docker build -t ghcr.io/conduit-obs/conduit-agent:dev -f deploy/docker/Dockerfile .

docker run --rm \
  -e HONEYCOMB_API_KEY="$HONEYCOMB_API_KEY" \
  -e CONDUIT_SERVICE_NAME=edge-host \
  -e CONDUIT_DEPLOYMENT_ENVIRONMENT=production \
  -p 127.0.0.1:4317:4317 \
  -p 127.0.0.1:4318:4318 \
  -p 127.0.0.1:13133:13133 \
  ghcr.io/conduit-obs/conduit-agent:dev
```

Tagged releases are published to `ghcr.io/conduit-obs/conduit-agent` (registry venue locked in [ADR-0019](../../docs/adr/adr-0019.md)) once the M4.C goreleaser pipeline is wired.

Or use the shipped compose:

```bash
export HONEYCOMB_API_KEY=hcaik_...
export CONDUIT_SERVICE_NAME=edge-host
docker compose -f deploy/docker/compose-linux-host.yaml up -d
```

Send a test span with [`otel-cli`](https://github.com/equinix-labs/otel-cli)
or your app's OTel SDK pointed at `http://127.0.0.1:4318` (from the host)
or `http://conduit:4318` (from a peer container in the same compose
project). Probe collector liveness with `curl http://127.0.0.1:13133`.

## What the image gives you

| Acceptance criterion (M4) | Where it's satisfied |
|---|---|
| Multi-arch (amd64 + arm64) | `dockers:` + `docker_manifests:` blocks in [`.goreleaser.yaml`](../../.goreleaser.yaml). Local source build via [`Dockerfile`](Dockerfile) honors `TARGETARCH` for buildx. |
| Runs as non-root by default | `USER nonroot:nonroot` (UID 65532) at the bottom of both Dockerfiles. |
| Health check endpoint reachable | `health_check` extension wired into the base template; bound to `0.0.0.0:13133`. |
| OTLP receivers bind to `0.0.0.0` in Docker profile | [`conduit.yaml.default`](conduit.yaml.default) sets `profile.mode: docker`; the expander emits `0.0.0.0:4317` / `:4318` for that mode and stays on `127.0.0.1` for every host mode. |
| Host metrics from a containerized agent (M9.A) | `profile.mode: docker` loads [`internal/profiles/docker/hostmetrics.yaml`](../../internal/profiles/docker/hostmetrics.yaml) with `root_path: /hostfs`. Operator's compose binds `/proc`, `/sys`, `/` to `/hostfs` and sets `pid: host` (see [`compose-linux-host.yaml`](compose-linux-host.yaml)); set `profile.host_metrics: false` to opt out. |

## Release pipeline (publish to ghcr.io)

Goreleaser does the multi-arch build at release time. Locally:

```bash
make release-snapshot
# or, equivalently:
goreleaser release --snapshot --clean --skip=publish
```

…produces both `ghcr.io/conduit-obs/conduit-agent:<snap>-amd64` and
`-arm64` images in the local docker daemon, which is the smoke-test path
for changes to either Dockerfile or the goreleaser blocks.

Tooling prerequisites (one-time):

```bash
brew install goreleaser           # macOS; or `go install github.com/goreleaser/goreleaser/v2@latest`
docker buildx version             # Docker Desktop ships buildx; verify it's on your PATH
```

In CI, a tagged release (e.g. `git tag v0.1.0 && git push --tags`) runs
goreleaser with publish enabled. The expected GitHub Actions workflow
(M12 deliverable) does:

1. `docker/setup-qemu-action` and `docker/setup-buildx-action`.
2. `docker/login-action` against `ghcr.io` with `GITHUB_TOKEN`.
3. `goreleaser/goreleaser-action` with the existing
   [`.goreleaser.yaml`](../../.goreleaser.yaml).

Tag scheme produced by goreleaser (matching the no-`v` convention used
by `honeycombio/refinery` and `otel/opentelemetry-collector-contrib`):

| Manifest tag | Per-arch tags |
|---|---|
| `:X.Y.Z` | `:X.Y.Z-amd64`, `:X.Y.Z-arm64` |
| `:X.Y` | `:X.Y-amd64`, `:X.Y-arm64` |
| `:latest` | `:latest-amd64`, `:latest-arm64` |

End users `docker pull ghcr.io/conduit-obs/conduit-agent:X.Y.Z` and
docker resolves the right arch via the manifest list automatically; the
per-arch tags exist mostly for CI plumbing and for users who want to
pin to one arch explicitly.

## Host metrics from the docker host (M9.A)

`profile.mode: docker` now ships a `hostmetrics.yaml` fragment that mirrors the linux shape — same scrapers, same `*.utilization` opt-ins, same `system.*` column vocabulary — but expects the operator's compose / `docker run` to bind-mount `/proc`, `/sys`, and `/` to `/hostfs` so the scrapers see the host's view rather than the container's. The contract is documented in [`internal/profiles/docker/README.md`](../../internal/profiles/docker/README.md) and the compose example below carries it out:

```yaml
services:
  conduit:
    image: ghcr.io/conduit-obs/conduit-agent:latest
    pid: host                                # unlock processes scraper
    volumes:
      - /proc:/hostfs/proc:ro,rslave
      - /sys:/hostfs/sys:ro,rslave
      - /:/hostfs/:ro,rslave                 # filesystem scraper mountpoints
    environment:
      HONEYCOMB_API_KEY: ${HONEYCOMB_API_KEY}
      CONDUIT_SERVICE_NAME: edge-host
      CONDUIT_DEPLOYMENT_ENVIRONMENT: production
      HOST_PROC_MOUNTINFO: /hostfs/proc/self/mountinfo
```

`pid: host` is what unlocks the `processes` scraper — without it, the container's PID namespace masks every PID outside its own. `HOST_PROC_MOUNTINFO` tells the filesystem scraper which mountinfo file lists the host's mountpoints (rather than the container's).

The privilege trade-off is real and intentional: the bind mounts give the agent visibility into the host's `/proc`, `/sys`, and root filesystem (read-only). For deployments where that's too much, set `profile.host_metrics: false` in a custom `conduit.yaml` to drop the fragment and stay OTLP-only — the bind mounts then become irrelevant.

[`compose-linux-host.yaml`](compose-linux-host.yaml) ships the full recipe end-to-end; running it produces a Conduit container that emits the same `system.*` metrics a `linux` host install would, plus all the OTLP traffic peer apps send via `conduit:4317` / `conduit:4318`.

The matching default board lives at [`dashboards/docker-host-overview.json`](../../dashboards/docker-host-overview.json) (M9.D).

## See also

- [`internal/profiles/docker/README.md`](../../internal/profiles/docker/README.md)
  for the V0 docker profile contract and what's deferred to M9.
- [`internal/expander/expander.go`](../../internal/expander/expander.go)
  §`resolveOTLPBindAddress` for the bind-address rule.
