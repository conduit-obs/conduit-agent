# Conduit

> An opinionated, OpenTelemetry-native agent distribution — a familiar, batteries-included install of the upstream OTel Collector with safe defaults, platform profiles, and a Datadog-style operator experience. Vendor-neutral on egress, with a first-class Honeycomb preset.

**Conduit is the bridge, not the destination.** It bundles the upstream OpenTelemetry Collector with the configuration ergonomics, packaging, and platform defaults that turn `apt-get install` into "host metrics, system logs, OTLP receivers — done." Egress is OTLP/HTTP or OTLP/gRPC to whatever observability backend you operate; named presets ship for the destinations the team uses most heavily (today: Honeycomb), and a generic `output.mode: otlp` covers everyone else (Datadog, Grafana Cloud, SigNoz, AWS ADOT, in-cluster collectors, …).

Conduit is a curated distribution of the upstream OpenTelemetry Collector plus a small CLI (`conduit`) that gives enterprise platform teams a familiar, batteries-included telemetry-collection experience with safe defaults. It runs on Linux, macOS, Docker, and Kubernetes; emits standard OTLP; and never locks customers in at the collection layer.

## Status

**Pre-alpha. Milestones M1, M2, M3, M4, and M5.A–M5.D done; M5.E (k8s default boards) in flight.**

| Step | Scope | Status |
|---|---|---|
| **M1** | Project skeleton, license, governance, ADRs, CLI scaffold, build/CI | done |
| **M2.A** | OCB manifest ([`builder-config.yaml`](builder-config.yaml)) + `make build-ocb` | done |
| **M2.B** | conduit.yaml schema (`internal/config/`), upstream-YAML expander (`internal/expander/`), `conduit config --validate`, `conduit preview` | done |
| **M2.C** | OCB output folded into `internal/collector/`, `conduit run` boots the embedded collector | done |
| **M3.A** | Platform default profiles: Linux + macOS host metrics, Linux journald + filelog, macOS filelog; auto-detection via `runtime.GOOS` | done |
| **M3.B** | Linux packaging: deb/rpm/apk/archlinux via nfpms, systemd unit, default `/etc/conduit/{conduit.yaml,conduit.env}`, idempotent maintainer scripts, `scripts/install_linux.sh` | done |
| **M4.A** | `health_check` extension always on at `0.0.0.0:13133` | done |
| **M4.B** | Docker profile (`profile.mode=docker`), self-contained `Dockerfile`, in-image default config, runnable compose example | done |
| **M4.C** | Multi-arch (amd64 + arm64) image build wired in `.goreleaser.yaml` for `ghcr.io/conduit-obs/conduit-agent` | done (publishing CI workflow lands at M12) |
| **M5.A** | `profile.mode: k8s` schema knob (binds OTLP to `0.0.0.0`); Helm chart skeleton at [`deploy/helm/conduit-agent/`](deploy/helm/conduit-agent/README.md): DaemonSet, ConfigMap, ServiceAccount, Service, optional Secret | done |
| **M5.B** | `profile.mode: k8s` defaults: per-node `hostmetrics`, `kubeletstats` against the local kubelet, `filelog/k8s` for `/var/log/pods/*` (with the upstream `container` operator), and `k8sattributes` enrichment on every pipeline. See [`internal/profiles/k8s/`](internal/profiles/k8s/README.md). | done |
| **M5.C** | Helm chart wiring for the M5.B receivers: read-only `ClusterRole` + `ClusterRoleBinding` (gated by `rbac.create`), DaemonSet host bind mounts (`/hostfs` with `HostToContainer` propagation, `/var/log/pods`, `/var/log/containers`) gated by `daemonset.hostMounts.enabled`, `runAsRoot` security context, plus `make kind-smoketest` to verify the chart end-to-end on a disposable kind cluster. | done |
| **M5.D** | Helm chart packaging + OCI publishing recipe (`make helm-package` / `make helm-publish`) targeting `oci://ghcr.io/conduit-obs/charts/conduit-agent` per ADR-0019, with cosign keyless-OIDC signing and a documented verification flow (`cosign verify-blob`). The first published version ships with v0.0.1; CI integration lands at M12. | done (CI hook lands at M12) |

The Docker default board (originally an M4 deliverable) is intentionally folded into M9 — the V0 docker profile is OTLP-only by design, so a docker host-overview shipping at M4 would have empty panels. M9 picks the host-metrics-from-container default and ships `dashboards/docker-host-overview.json` with real columns to plot.

M5 ships in slices, mirroring the Linux / Docker pattern: M5.A was the **chart skeleton + schema knob** (`helm install` works as an OTLP relay), M5.B wired the **kubelet / container-log / `k8sattributes` defaults**, M5.C added **RBAC + host bind mounts** so those receivers actually have access to what they need plus the kind smoke recipe to prove it, M5.D (this milestone) lands the **chart packaging + OCI publishing recipe** with cosign signing. M5.E ships the default cluster + workload boards. See [`deploy/helm/README.md`](deploy/helm/README.md) for the slice plan.

You can run, against any conduit.yaml:

```sh
./bin/conduit config --validate -c conduit.yaml   # exits 0 + "valid", or non-zero with structured field issues
./bin/conduit preview            -c conduit.yaml   # prints the rendered upstream OTel Collector YAML
./bin/conduit run                -c conduit.yaml   # boots the embedded collector; OTLP on :4317/:4318 plus profile receivers
```

`./bin/conduit doctor`, `./bin/conduit version`, and `./bin/conduit send-test-data` still exit non-zero with `not implemented; see milestone <Mn>` and land in later milestones.

## Install on Linux

Once we cut a tagged release, the one-liner installer pulls the right
deb / rpm / apk for the host:

```sh
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
  | sudo bash -s -- --api-key="$HONEYCOMB_API_KEY" --service-name=edge-gateway
```

What gets installed:

- `/usr/bin/conduit` (the agent binary, with the embedded collector).
- `/etc/conduit/conduit.yaml` (default config, references env vars; 0640 root:conduit).
- `/etc/conduit/conduit.env` (`HONEYCOMB_API_KEY`, `CONDUIT_SERVICE_NAME`, `CONDUIT_DEPLOYMENT_ENVIRONMENT`; 0640 root:conduit).
- `/lib/systemd/system/conduit.service` (deb) or `/usr/lib/systemd/system/conduit.service` (rpm/arch).
- A system `conduit:conduit` user, added to `adm` and `systemd-journal`.
- `/var/lib/conduit/` (reserved for filestorage queues, M10) and `/var/log/conduit/`.

Defaults follow the same OS-detection rules as the rest of Conduit: no
`profile:` block in your config means hostmetrics + filelog/system +
journald on Linux. Logs go to journald (`journalctl -u conduit`).

For the manual-install path and packaging internals, see
[`deploy/linux/README.md`](deploy/linux/README.md).

## Run on Docker

Build the image from the repo and run it as a sidecar / standalone agent
that accepts OTLP from peer containers:

```sh
docker build -t ghcr.io/conduit-obs/conduit-agent:dev -f deploy/docker/Dockerfile .
docker compose -f deploy/docker/compose-linux-host.yaml up -d
```

The image is `gcr.io/distroless/static-debian12:nonroot` (UID 65532, no
shell) with the conduit binary statically linked. The default in-image
[`conduit.yaml`](deploy/docker/conduit.yaml.default) sets `profile.mode:
docker`, which makes the expander bind OTLP receivers to `0.0.0.0:4317`
/ `:4318` so peer containers in the same docker network can reach the
agent. The compose file publishes those ports to `127.0.0.1` on the host
only — LAN-wide ingest is an explicit opt-in (change the bind address
on the `ports:` mappings).

The `health_check` extension is always on at `0.0.0.0:13133`. Probe with
`curl http://127.0.0.1:13133` (200 = every pipeline is up) for compose,
or HTTP-GET it from k8s liveness/readiness probes.

For multi-arch publishing, the bind-mount story for monitoring the
docker host's CPU / memory / disk from a containerized agent, and the
default-dashboard plan, see [`deploy/docker/README.md`](deploy/docker/README.md).

## Run on Kubernetes

Install the chart from the repo as a DaemonSet that accepts OTLP from
peer pods at `<release>-conduit-agent:4317` / `:4318`:

```sh
kubectl create namespace conduit
kubectl -n conduit create secret generic conduit-honeycomb \
  --from-literal=HONEYCOMB_API_KEY=hcaik_...

helm install conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=edge-cluster-prod \
  --set honeycomb.existingSecret=conduit-honeycomb
```

The chart sets `profile.mode: k8s` by default, which makes the expander
bind OTLP to `0.0.0.0:4317` / `:4318` so peer pods can reach the agent
through the cluster Service. Each pod also exposes the `health_check`
extension at `:13133` for liveness / readiness probes (the chart wires
both probes against it automatically).

M5.A through M5.C are landed: the DaemonSet ships per-node
`hostmetrics` (rooted at `/hostfs` via the chart bind mount),
`kubeletstats` against the local kubelet, `filelog/k8s` for
`/var/log/pods/*`, and the `k8sattributes` processor on every pipeline
so OTLP signals from instrumented apps in the cluster also pick up
Kubernetes workload metadata. The chart provisions a read-only
`ClusterRole` for those receivers (toggle with `rbac.create=false` if
you manage RBAC out-of-band) and bind-mounts `/hostfs`, `/var/log/pods`,
and `/var/log/containers` from the host (toggle with
`daemonset.hostMounts.enabled=false` for an OTLP-only relay). Verify
end-to-end on a disposable kind cluster with `make kind-smoketest`. OCI
publishing of the chart lands in M5.D; the default cluster + workload
boards in M5.E.

Send to a different backend by flipping the egress mode at install
time:

```sh
# Generic OTLP/HTTP (Datadog OTLP intake; the same shape works for
# Grafana Cloud, SigNoz, AWS ADOT, etc. — change endpoint + headers).
helm install conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=edge-cluster-prod \
  --set otlp.enabled=true \
  --set otlp.endpoint=https://otlp.us5.datadoghq.com \
  --set 'otlp.headers.DD-API-KEY=${env:DD_API_KEY}' \
  --set 'extraEnv[0].name=DD_API_KEY' \
  --set 'extraEnv[0].valueFrom.secretKeyRef.name=datadog-otlp' \
  --set 'extraEnv[0].valueFrom.secretKeyRef.key=api-key'

# OTLP/gRPC to a customer-operated gateway collector.
helm install conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=edge-cluster-prod \
  --set gateway.enabled=true \
  --set gateway.endpoint=otel-gateway.observability.svc:4317
```

For values reference, RBAC plan, and the full egress / extraEnv guide,
see [`deploy/helm/conduit-agent/README.md`](deploy/helm/conduit-agent/README.md).

## Host identity (always on)

Every signal Conduit emits — host metrics, system logs, OTLP from your apps — is auto-tagged with the standard OpenTelemetry host resource attributes via `resourcedetectionprocessor`:

- `host.name`, `host.id`, `host.arch`, `host.ip`, `host.mac`
- `os.type`, `os.description`

This is wired into every pipeline regardless of profile, because identifying which host emitted a metric is a precondition for any multi-host dashboard.

To override (e.g., to use a Kubernetes node name instead of the pod hostname) set `OTEL_RESOURCE_ATTRIBUTES` in the environment before starting Conduit:

```sh
export OTEL_RESOURCE_ATTRIBUTES="host.name=$NODE_NAME"
```

Detector order is `[env, system]` with `override: false`, so user-supplied values win and incoming OTLP signals from upstream apps are left untouched.

## Default log parsing (always on for filelog)

The platform-default `filelog/system` receiver runs a regex parser over every line so Honeycomb gets structured columns instead of an opaque `body` blob:

| Attribute | Source |
|---|---|
| `process` | The process name from `proc[pid]:` |
| `pid` | The PID inside the brackets, when present |
| `message` | Everything after `proc[pid]:` |

Two timestamp formats are supported out of the box: BSD syslog (`May  1 13:59:13 ...`, used by `/var/log/syslog`, `/var/log/messages`, macOS `system.log`, etc.) and the ISO-with-offset format macOS uses for `install.log` (`2026-05-01 13:42:38-04 ...`). Lines that don't match either are forwarded unparsed.

On top of the parsed `message`, a `transformprocessor` on the logs pipeline computes a `normalized_message` attribute by masking the bits that make every line unique:

- UUIDs → `*`
- IPv4 addresses → `*.*.*.*`
- `key=value` pairs → `key=*`
- Standalone integers ≥ 4 digits (PIDs, IDs, timestamps) → `*`

`body` and `message` are left untouched. Group by `normalized_message` to count "templates" of similar log lines:

```
"Adding client SUUpdateServiceClient pid=*, uid=*, installAuth=NO rights=(), transactions=*"
"Removing client SUUpdateServiceClient pid=*, uid=*, installAuth=NO rights=(), transactions=*"
```

…instead of seeing each line as unique.

The same `transformprocessor` also defaults `severity_text` to `INFO` and `severity_number` to `9` (`SEVERITY_NUMBER_INFO`) when the record arrives without one — guarded by `where severity_number == SEVERITY_NUMBER_UNSPECIFIED` so journald entries (which carry `PRIORITY` -> severity) and OTLP logs from upstream apps pass through with their original severity intact.

## Default profiles

Out of the box (omit `profile:` from your conduit.yaml entirely), Conduit detects the host OS and turns on the matching platform fragment set:

| Fragment | Linux | macOS (darwin) | Windows | Docker | Kubernetes |
|---|---|---|---|---|---|
| `hostmetrics` (CPU / memory / load / disk / filesystem / network) | yes | yes | M6 | M9 (needs bind mounts) | yes (per-node via DaemonSet; needs chart bind mounts in M5.C) |
| `hostmetrics` (paging / processes) | yes | no (privilege-sensitive on darwin) | M6 | M9 | yes (M5.C bind mounts) |
| `filelog/system` — `/var/log/{syslog,messages,auth.log,secure}` | yes | — | — | — | — |
| `filelog/system` — `/var/log/{system,install}.log` | — | yes | — | — | — |
| `journald` (systemd unified journal) | yes | — | — | — | — |
| `kubeletstats` (pod / container CPU + memory) | — | — | — | — | yes (needs chart RBAC in M5.C) |
| `filelog/k8s` of `/var/log/pods/*` + `k8sattributes` enrichment | — | — | — | — | yes |

The hostmetrics scrapers also enable the `*.utilization` metrics that upstream ships disabled by default — `system.cpu.utilization`, `system.memory.utilization`, `system.filesystem.utilization`, and (Linux only) `system.paging.utilization`. These are 0..1 fractions computed alongside the byte-level metrics, so dashboards can plot percent-used directly instead of dividing the raw `system.filesystem.usage` bytes by the total themselves.

Knobs:

```yaml
profile:
  mode: auto          # auto | linux | darwin | docker | k8s | none (default: auto)
  host_metrics: true  # default true unless mode=none / mode=docker
  system_logs: true   # default true unless mode=none / mode=docker
```

`mode: none` reverts to the M2 OTLP-only behavior. `mode: linux` / `mode: darwin` force a profile regardless of the host OS — useful in containers or when developing a Linux config on a Mac. `mode: docker` and `mode: k8s` are the container-native shapes — both flip OTLP receivers to `0.0.0.0` so peer containers / pods can reach the agent (see "OTLP bind address" below). `docker` ships no platform fragment receivers in V0 (M9 will pick the host-metrics-from-container default once the bind-mount story is settled). `k8s` ships per-node `hostmetrics`, `kubeletstats` against the local kubelet, `filelog/k8s` for container logs, and the `k8sattributes` processor on every pipeline. On a runtime.GOOS without a fragment set (today: anything not linux or darwin), `auto` falls back to `none` and writes a one-line warning to stderr.

The fragment YAML lives under [`internal/profiles/<goos>/`](internal/profiles/) — each file is plain upstream OTel Collector receiver config that the expander splices into the rendered pipeline.

### OTLP bind address

The OTLP receivers listen on `127.0.0.1:4317` / `:4318` for every host-mode profile (`auto`, `linux`, `darwin`, `none`) and on `0.0.0.0:4317` / `:4318` for the container-native profiles (`mode: docker`, `mode: k8s`). The host default is the safe one: a stock `apt-get install conduit-agent` does not turn the host into an OTLP relay for the local network. Apps on the same machine still reach the agent via the loopback interface; LAN-wide / cluster-wide ingest is an explicit opt-in via `profile.mode: docker` (containers) or `profile.mode: k8s` (Helm chart, where peer pods reach the DaemonSet via the cluster network).

This is enforced in the rendered config: `internal/expander/expander.go` derives the bind address from `profile.mode` directly — there's no separate bind-address knob to forget to set.

### `overrides:` escape hatch

`conduit.yaml` is intentionally narrow — every field has been weighed against the schema-creep risk that turns vendor distributions into thinly-disguised OTel YAML. When you genuinely need to reach an upstream OTel Collector knob Conduit hasn't surfaced (a non-default scraper interval, a third-party processor like `redaction`, an extra exporter, a tweak to a pipeline's processor list), drop into the top-level `overrides:` block:

```yaml
service_name: demo
deployment_environment: prod
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}

overrides:
  receivers:
    kubeletstats:
      collection_interval: 15s          # bump from the 60s default
  processors:
    redaction:
      allow_all_keys: true
      blocked_values: ['(?i)password=\S+']
  service:
    pipelines:
      logs:
        # Lists replace wholesale (collector multi-config merge semantics),
        # so restate the full pipeline order to splice your processor in.
        processors: [memory_limiter, resourcedetection, k8sattributes,
                     resource, transform/logs, redaction, batch]
```

How it works mechanically: the expander emits the user's `overrides:` block as a *second* `yaml:` config source to the embedded Collector — the Collector's standard multi-config resolver deep-merges them at startup (maps merge by key, lists replace wholesale). Conduit doesn't ship its own deep-merge code; the merge semantics are exactly what `otelcol --config base.yaml --config overrides.yaml` would do. `conduit preview` shows the two documents separated by `---` so you can see what's layering on top of what.

Top-level keys outside the standard collector vocab (`receivers` / `processors` / `exporters` / `connectors` / `extensions` / `service`) are validation errors at load time, so a typo doesn't silently no-op. See [ADR-0012](docs/adr/adr-0012.md) for the design rationale and the review cadence — heavy override patterns are signal that the schema is missing a first-class field, not a normal use case.

## Default dashboards

The Honeycomb boards Conduit ships out of the box live as checked-in JSON under [`dashboards/`](dashboards/):

- [`macos-host-overview.json`](dashboards/macos-host-overview.json) — per-host overview for the darwin profile.
- [`linux-host-overview.json`](dashboards/linux-host-overview.json) — Linux equivalent, with platform-appropriate filters (block-device-only filesystems, loopback excluded from network) and a swap-utilization panel that macOS deliberately omits.

Future platform boards (Docker, Windows, k8s) land alongside their install milestones (M5 / M6) and at M9 for the Docker host overview, which depends on M9's host-metrics-from-container work. Each board is its own opinionated, narrative-driven dashboard tailored to what's distinctive about that platform's telemetry — a k8s board keys off pods and namespaces, a docker board off containers and compose services, a Windows board off Event Log channels — rather than a forced lowest-common-denominator panel skeleton. The cross-platform contract in [`internal/profiles/PROFILE_SPEC.md`](internal/profiles/PROFILE_SPEC.md) holds the *telemetry shape* (column names, severity defaults, resource attributes) constant; §3 of that doc spells out the dashboard quality bar boards are reviewed against.

The CLI subcommand to apply these (`conduit board apply`, M11) reads them, but the file format is intentionally close to Honeycomb's `/1/boards` API so operators can also POST them by hand against `HONEYCOMB_CONFIG_API_KEY` (a *configuration* key — distinct from the ingest key in `conduit.yaml`). See [`dashboards/README.md`](dashboards/README.md) for the schema and auth model.

## What Conduit is

A familiar, batteries-included OTel Collector distribution and CLI that:

1. Lets a platform engineer with no OpenTelemetry expertise turn telemetry on in 30 minutes.
2. Sends Honeycomb-shaped data with safe defaults (cardinality-aware, redaction-aware, RED-metrics-before-sampling).
3. Stays open: pure upstream OTel components in V0, no proprietary exporter, no lock-in at the collection layer.

## What Conduit is not

- **Not a replacement for the OTel Collector.** Conduit is a curated distribution of it.
- **Not a replacement for Honeycomb.** Conduit's success is measured in how quickly customers feel productive in Honeycomb.
- **Not a control plane.** No fleet management, remote config, or policy server in V0.
- **Not a gateway tier.** V0 ships only the agent. Customers needing a gateway run the Honeycomb Collector or any OTLP-capable gateway. Conduit emits to one with a single config switch.
- **Not a fork.** Conduit composes upstream components via the OpenTelemetry Collector Builder (OCB).

## Architecture Decision Records

The decisions that lock V0's shape are committed under [`docs/adr/`](docs/adr/) (`adr-0001.md` through `adr-0019.md`). Each ADR captures one decision, its alternatives, and its consequences. Read them in order to understand the build doctrine — Apache-2.0 + clean-room (ADR-0013), pure upstream OTel components (ADR-0004), `output.mode` rather than per-signal endpoints (ADR-0008), allowlist-based RED dimensions (ADR-0006), and so on. New decisions get a new ADR.

## V0 / V1 / V2 at a glance

| Phase | Theme | Headline | Not in this phase |
|---|---|---|---|
| **V0** | Adoption bridge (agent only) | OCB-based distribution; Linux/Windows/Docker/K8s Helm; AWS recipes; declarative `output:` block (`honeycomb` or `gateway`, with optional Refinery for traces); RED metrics before sampling; `conduit doctor` and `conduit preview`; safe-by-default cardinality and redaction | Conduit-as-gateway; agent-side fanout; S3 archival; remote config; fleet inventory; Lambda extension |
| **V1** | Field hardening | Translation guides, broader k8s receivers, redaction profile library, signed-release maturity | Anything requiring a control plane |
| **V2+** | Control plane and gateway role | Enrollment, remote config, fleet inventory, rollout rings, drift detection, policy packs, Conduit-as-gateway with routing fanout | — |

## Build

A fresh clone builds with no extra steps — `internal/collector/components.go` (the OCB-generated factory map) is committed:

```sh
make build            # builds ./bin/conduit including the embedded OTel collector
make test             # go test ./...
make lint             # golangci-lint run ./...
make release-snapshot # local-only goreleaser snapshot; no publish, no signing
```

When you change [`builder-config.yaml`](builder-config.yaml) (add a component, bump upstream version), regenerate the embedded collector:

```sh
make install-ocb      # one-time download of the pinned OCB binary into ./bin/ocb
make build-ocb        # regenerate internal/collector/components.go from builder-config.yaml
                      #   (under the hood: runs OCB to ./build/collector/, then folds in
                      #    components.go with the package rewritten to "package collector")
```

You can also run:

```sh
./bin/conduit --help                          # lists every subcommand
./bin/conduit config --validate -c FILE       # validates conduit.yaml against the schema
./bin/conduit preview -c FILE                 # renders the upstream OTel Collector YAML
./bin/conduit run -c FILE                     # boots the embedded collector
```

See [Makefile](Makefile) for the full target list.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). The two rules engineers need to know on day one:

1. **No verbatim code copy from the Observe Agent reference.** Patterns may be borrowed conceptually with attribution; verbatim copy is a rejected PR.
2. **No custom OTel processors or receivers in V0.** Pure upstream only (ADR-0004).

## Security

Report vulnerabilities to `security@conduit-obs.com`. See [`SECURITY.md`](SECURITY.md) for the disclosure process.

## License

[Apache-2.0](LICENSE). Copyright 2026 The Conduit Authors.
