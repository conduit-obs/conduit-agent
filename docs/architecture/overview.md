# Architecture overview

This page is the public-facing tour of what runs inside the Conduit
agent and how the pieces fit together. It's pitched at platform
engineers and SREs who'll operate Conduit, not at people who'll
hack on its internals (for that, see the ADRs at
[`docs/adr/`](../adr/) and the milestone plan in the source tree).

## What Conduit is

Conduit is a vendor-agnostic OpenTelemetry Collector distribution
shipped as a single agent. You write a small `conduit.yaml`, the
agent renders that into a full upstream Collector configuration at
startup, and the embedded Collector does the heavy lifting from
there. The whole purpose is to give you a 30-minute path from "I
just provisioned a host" to "metrics, traces, and logs are landing
in Honeycomb (or any other OTLP destination)" — without having to
become a Collector expert.

## High-level shape

```
                            ┌─────────────────────────────┐
   conduit.yaml ──parse──▶  │   conduit binary            │   ──▶ Honeycomb
   (small, typed)           │   ┌──────────────────────┐  │       (or any
                            │   │  expander            │  │        OTLP sink)
   profile fragments        │   │  (templates +        │  │
   (per-OS YAML)     ───▶   │   │   profile fragments) │  │
                            │   └──────────┬───────────┘  │
                            │              │              │
                            │              ▼              │
                            │   ┌──────────────────────┐  │
                            │   │ embedded OTel        │  │
                            │   │ Collector (otelcol)  │  │
                            │   │  receivers/processors│  │
                            │   │  /connectors/exports │  │
                            │   └──────────────────────┘  │
                            └─────────────────────────────┘
```

Three things to internalize:

1. **`conduit.yaml` is the surface area you maintain.** It's small,
   typed, and validated. See
   [`docs/reference/configuration.md`](../reference/configuration.md).
2. **The expander is the translation layer.** It takes
   `conduit.yaml` plus the resolved profile and produces a
   fully-formed collector YAML. You can see exactly what it'll
   produce with `conduit preview`. The translation is deterministic
   — same input → same output, byte for byte (we test this with the
   golden-file suite at
   [`internal/expander/testdata/goldens/`](../../internal/expander/testdata/goldens/README.md)).
3. **The embedded Collector is upstream OpenTelemetry.** No forks,
   no proprietary processors. Conduit pins specific upstream
   versions (see
   [`docs/release/compatibility.md`](../release/compatibility.md))
   and ships them as a single static binary built with the
   OpenTelemetry Collector Builder (OCB).

## Lifecycle of a single signal

Trace a span from your app to Honeycomb. The path is
representative for metrics and logs too — the receiver block
changes, the rest is shared.

```
your app ── OTLP/HTTP ──▶ [otlp receiver]
                            │
        ┌───────────────────┴───────────────────┐
        │ (the receiver fans every span out      │
        │  to both traces pipelines)             │
        ▼                                        ▼
  traces (main)                            traces/red (RED derivation)
  [memory_limiter]                         [memory_limiter]
        │                                        │
        ▼                                        ▼
  [resourcedetection]                      [resourcedetection]
        │                                        │
        ▼                                        ▼
  [k8sattributes] (k8s only)               [k8sattributes] (k8s only)
        │                                        │
        ▼                                        ▼
  [resource]                               [resource]
        │                                        │
        ▼                                        ▼
  [batch]                                  [filter/red_requests]
        │                                  (keep Server / Consumer spans)
        ▼                                        │
  [otlphttp/honeycomb]                           ▼
        │                                  [batch]
        ▼                                        │
    Honeycomb  ◀── every span                    ▼
                                           [span_metrics connector]
                                                 │
                                                 ▼
                                           metrics pipeline ──▶ Honeycomb
                                           (RED: rate / errors / duration)
```

The main traces pipeline ships **every** span to Honeycomb for rich
troubleshooting; the `traces/red` pipeline derives RED metrics from
request-like spans only and never mutates the trace stream. Logs flow
through their own pipeline (the `transform/logs` processor runs there,
not on traces). See [ADR-0022](../adr/adr-0022.md).

### The pipeline cast, briefly

| Component | Role |
|---|---|
| `otlp` receiver | The always-on ingress. Accepts gRPC on `:4317` and HTTP on `:4318`. Bound to `127.0.0.1` on host installs (Linux / macOS / Windows) and to `0.0.0.0` on Docker / k8s profiles where peer containers / pods need to reach it. |
| `memory_limiter` processor | First processor in every pipeline. Backpressures receivers when RSS approaches `limit_mib` (default 1500MiB on amd64). |
| `resourcedetection` processor | Adds `host.*`, `os.*`, `cloud.*` attributes via the standard OTel detectors (system, ec2, gcp, azure, k8s). |
| `k8sattributes` processor | k8s profile only. Enriches signals with `k8s.deployment.name`, `k8s.daemonset.name`, `k8s.pod.uid`, etc., correlating by source IP / pod UID / connection. |
| `resource` processor | Pins `service.name` + `deployment.environment` from `conduit.yaml` if the SDK didn't set them. |
| `transform/logs` processor | Logs pipeline only. Runs OTTL rules to redact common credential shapes (`Authorization` headers, AWS access key IDs, etc.). See [ADR-0010](../adr/adr-0010.md). |
| `filter/red_requests` processor | `traces/red` pipeline only. Keeps only `Server` / `Consumer` spans so RED is derived from inbound serviced requests, not client / internal / producer spans. See [ADR-0022](../adr/adr-0022.md). |
| `span_metrics` connector | Derives RED metrics (request count, error count via `status.code`, duration histogram) from the dedicated `traces/red` pipeline before any sampling step, so derived metrics see 100% of request traffic. See [ADR-0006](../adr/adr-0006.md) and [ADR-0022](../adr/adr-0022.md). |
| `batch` processor | Batches by size + age. Standard upstream defaults; the `overrides:` escape hatch is the way to retune. |
| `otlphttp/honeycomb` (or `otlphttp` / `otlp/refinery` / `otlp/gateway`) exporter | The egress. Carries `x-honeycomb-team` for the Honeycomb preset, or whatever headers `output.otlp.headers:` declares. |

## Receivers shipped

| Profile | Receivers loaded by default |
|---|---|
| linux | `otlp`, `hostmetrics` (cpu, memory, filesystem, network, load, processes), `journald`, `filelog` (`/var/log/syslog`, `/var/log/messages`) |
| darwin | `otlp`, `hostmetrics` (cpu, memory, filesystem, network, load), `filelog` (`/var/log/system.log`, console) |
| windows | `otlp`, `hostmetrics` (cpu, memory, filesystem, network, paging, processes), `windowseventlog/application`, `windowseventlog/system`. Security channel is **not** loaded by default; see the [Windows getting-started](../getting-started/windows.md#step-5--enable-the-security-event-log-advanced) for how to opt in. |
| docker | `otlp` only (bound to `0.0.0.0:4317` / `:4318`). Host metrics require the bind-mount + `--pid=host` recipe in the [Docker guide](../getting-started/docker.md). |
| k8s | `otlp` (bound to `0.0.0.0`), `hostmetrics` (per-node), `kubeletstats`, `filelog/k8s` (reads `/var/log/pods/`). The **per-node** profile (Helm DaemonSet). |
| k8s-cluster | `otlp` (bound to `0.0.0.0`), `k8s_cluster` (cluster-state metrics), `k8sobjects` (Kubernetes events → logs). The **cluster-singleton** companion to `k8s` (Helm single-replica Deployment); these receivers render inline rather than as fragments and must run once per cluster. |
| none | `otlp` only. Use this when Conduit runs as a sidecar that should only forward what apps explicitly send it. |

The fragments live under
[`internal/profiles/<mode>/`](../../internal/profiles/) — each is a
plain YAML file the expander splices into the rendered config.

## The output block

Three modes, three intents:

```
                       output.mode = honeycomb
your app ──▶ conduit ────────────────────────▶ api.honeycomb.io
                              │
                       (x-honeycomb-team header pre-wired,
                        OTLP/HTTP, gzip)


                       output.mode = otlp
your app ──▶ conduit ────────────────────────▶ any OTLP/HTTP
                              │                  destination
                       (caller-supplied             (Datadog, Grafana,
                        endpoint + headers)         SigNoz, AWS ADOT, …)


                       output.mode = gateway
your app ──▶ conduit ────────────────────────▶ customer-operated
                              │                  OTLP/gRPC gateway
                       (gRPC, batch, optional       (which then fans out)
                        x-* tenant headers)
```

When `output.mode: honeycomb` and `output.honeycomb.traces.via_refinery`
is set, the traces pipeline gets a second exporter
(`otlp/refinery`) and the metrics + logs pipelines stay on the
direct Honeycomb path. So a single config can do "metrics direct,
traces sampled by Refinery" — see
[`docs/reference/configuration.md#outputhoneycomb-when-mode-honeycomb`](../reference/configuration.md#outputhoneycomb-when-mode-honeycomb).

## Persistent queue

When `output.persistent_queue.enabled: true`, every exporter gets a
`sending_queue.storage: file_storage` block, and a
`file_storage` extension is loaded that points at
`output.persistent_queue.dir`. The agent persists in-flight OTLP
batches to disk, so a restart while batches were queued doesn't
lose them.

The trade-offs are real and Conduit doesn't hide them:

- The dir must be on a persistent volume (validation rejects
  `/tmp` and `/dev/shm`).
- On ECS Fargate / immutable container hosts there's no useful
  persistent dir — leave it off and rely on the SDK's retry budget.
- After a long network outage the on-disk queue can grow unbounded.
  The `CDT0401 queue.health` check (M11 follow-up) will surface
  queue depth and trigger eviction once it ships.

## RED metrics from spans

The `span_metrics` connector is fed by the dedicated `traces/red`
pipeline, which shares the `otlp` receiver with the main traces
pipeline — so RED metrics are derived **before** any sampling step and
see 100% of traffic even when operators tail-sample downstream.
`traces/red` applies `filter/red_requests` first, so RED counts only
request-like (`Server` / `Consumer`) spans; the main traces pipeline
still ships every span to the destination unchanged
([ADR-0022](../adr/adr-0022.md)). The default dimension set
(`service.name`, `deployment.environment`, `http.route`, the HTTP
method + status-code attributes in both stable and legacy semconv
form, `rpc.*`, `messaging.*`) is deliberately conservative — every
entry has been weighed against a
[cardinality denylist](../reference/configuration.md#cardinality-denylist)
that rejects per-request and per-user attributes at config-load
time.

`cardinality_limit` (default 5000) caps the total
dimension-combination fan-out. Excess combinations land in the
`otel.metric.overflow="true"` series instead of unbounded
allocation.

The whole RED feature is documented in [ADR-0006](../adr/adr-0006.md)
and configured under
[`metrics.red`](../reference/configuration.md#metricsred).

## How configuration is rendered

The expander is a Go template engine. It composes:

1. The base template
   ([`internal/expander/templates/base.yaml.tmpl`](../../internal/expander/templates/base.yaml.tmpl))
   — receivers, processors, connectors, exporters, pipeline wiring.
2. Profile fragments — small YAML files per OS, spliced into the
   right sections (e.g. `internal/profiles/linux/hostmetrics.yaml`
   adds the `hostmetrics` receiver and wires it into the metrics
   pipeline).
3. The user's `conduit.yaml` — service name, environment, output
   block, optional RED tuning, optional `overrides:`.

The rendered output is a single upstream-compatible
`otelcol-config.yaml`. You can see it with `conduit preview`. The
golden-file suite (10 canonical scenarios under
[`internal/expander/testdata/goldens/`](../../internal/expander/testdata/goldens/README.md))
asserts the byte-for-byte output for each — that's our regression
guard against silent template drift.

When `overrides:` is non-empty, the rendered base + the overrides
block are passed to the embedded collector as **two separate config
sources**. The collector itself does the deep-merge at startup, with
overrides winning on key conflicts and lists replacing rather than
concatenating (matching upstream multi-config semantics). See
[ADR-0012](../adr/adr-0012.md) for why we don't merge in the
expander.

## Diagnostics: `conduit doctor`

The doctor framework lives at
[`internal/doctor/`](../../internal/doctor/). It runs a catalog of
checks (config syntax, output reachability, output auth, output TLS
posture, receiver port availability, receiver permission posture,
RED cardinality warnings, version compatibility) and reports a
structured `Result` per check with a stable `CDT0xxx` ID.

```
conduit doctor
├─ CDT0001 config.syntax            ─▶ wraps internal/config.Validate
├─ CDT0201 receiver.ports           ─▶ probes 4317/4318 + identifies conflicting PIDs (Linux)
├─ CDT0202 receiver.permissions     ─▶ verifies filelog include paths are readable
├─ CDT0102 output.auth              ─▶ validates API key presence (never logs it)
├─ CDT0103 output.tls_warning       ─▶ warns when insecure: true is set
├─ CDT0101 output.endpoint_reachable─▶ TCP+TLS handshake against the resolved egress URL
├─ CDT0501 config.cardinality_warnings ─▶ surfaces RED denylist hits
└─ CDT0403 version.compat           ─▶ informational PASS with conduit + collector-core + GOOS/GOARCH
```

Every check ID has a heading in
[`docs/troubleshooting/cdt-codes.md`](../troubleshooting/cdt-codes.md);
a CI test (`TestDocsAnchorParity`) blocks merges that add a check
without a docs section. The reverse is also true — reserved codes
(`CDT0301` k8s permissions, `CDT0401` queue health, `CDT0402`
memory pressure, `CDT0510` cardinality observed) all have headings
even though the check is a future M11 follow-up; that way the
docs stay green while the runtime side fills in.

## Single binary, multiple platforms

The agent is one Go binary built with OCB, distributed as:

| Platform | Format | Path |
|---|---|---|
| Linux (deb / rpm / apk / pacman) | nfpms-built packages | [`deploy/linux/`](../../deploy/linux/) |
| Docker | distroless static image | [`deploy/docker/`](../../deploy/docker/) |
| Kubernetes | Helm chart | [`deploy/helm/conduit-agent/`](../../deploy/helm/) |
| Windows | WiX-built MSI | [`deploy/windows/`](../../deploy/windows/) |
| Bare binaries | per-arch tarballs | published with each GitHub release |

Releases are reproducible: the SBOM, sigstore Cosign signature, and
GPG-signed git tag all flow through goreleaser. See
[`docs/release/runbook.md`](../release/runbook.md) for the cut
process and [`docs/release/compatibility.md`](../release/compatibility.md)
for the version-pinning policy.

## Where to read more

- **`conduit.yaml` schema** —
  [`docs/reference/configuration.md`](../reference/configuration.md)
- **Per-platform install** — [`docs/getting-started/`](../getting-started/)
- **Architecture decisions (ADRs)** — [`docs/adr/`](../adr/)
- **Release process** — [`docs/release/`](../release/)
- **Troubleshooting** — [`docs/troubleshooting/`](../troubleshooting/)
