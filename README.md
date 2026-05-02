# Conduit

> An opinionated, OpenTelemetry-native agent distribution — a familiar, batteries-included install of the upstream OTel Collector with safe defaults, platform profiles, and a Datadog-style operator experience. Vendor-neutral on egress, with a first-class Honeycomb preset.

**Conduit is the bridge, not the destination.** It bundles the upstream OpenTelemetry Collector with the configuration ergonomics, packaging, and platform defaults that turn `apt-get install` into "host metrics, system logs, OTLP receivers — done." Egress is OTLP/HTTP or OTLP/gRPC to whatever observability backend you operate; named presets ship for the destinations the team uses most heavily (today: Honeycomb), and a generic `output.mode: otlp` covers everyone else (Datadog, Grafana Cloud, SigNoz, AWS ADOT, in-cluster collectors, …).

Conduit is a curated distribution of the upstream OpenTelemetry Collector plus a small CLI (`conduit`) that gives platform teams a familiar, batteries-included telemetry-collection experience with safe defaults. It runs on Linux, macOS, Docker, Kubernetes, and Windows; emits standard OTLP; and never locks customers in at the collection layer.

## What Conduit is

A familiar, batteries-included OTel Collector distribution and CLI that:

1. Lets a platform engineer with no OpenTelemetry expertise turn telemetry on in 30 minutes.
2. Sends safe-by-default data — cardinality-aware, redaction-aware, RED-metrics-before-sampling.
3. Stays open: pure upstream OTel components, no proprietary exporter, no lock-in at the collection layer.

## What Conduit is not

- **Not a replacement for the OTel Collector.** Conduit is a curated distribution of it.
- **Not a control plane.** No fleet management, remote config, or policy server in V0.
- **Not a gateway tier.** V0 ships only the agent. Customers needing a gateway run any OTLP-capable gateway. Conduit emits to one with a single config switch.
- **Not a fork.** Conduit composes upstream components via the OpenTelemetry Collector Builder (OCB).

## Quick install

### Linux

```sh
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
  | sudo bash -s -- --api-key="$HONEYCOMB_API_KEY" --service-name=edge-gateway
```

Installs the agent binary, a systemd unit, a `conduit:conduit` system user, and the default config at `/etc/conduit/conduit.yaml`. Logs go to journald (`journalctl -u conduit`). See [`docs/getting-started/linux.md`](docs/getting-started/linux.md).

### Docker

```sh
docker compose -f deploy/docker/compose-linux-host.yaml up -d
```

The default in-image config sets `profile.mode: docker` so peer containers send OTLP to the agent at `:4317` / `:4318`. The `health_check` extension is reachable at `:13133`. See [`docs/getting-started/docker.md`](docs/getting-started/docker.md).

### Kubernetes

```sh
kubectl create namespace conduit
kubectl -n conduit create secret generic conduit-honeycomb \
  --from-literal=HONEYCOMB_API_KEY=hcaik_...

helm install conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=edge-cluster-prod \
  --set honeycomb.existingSecret=conduit-honeycomb
```

Per-node DaemonSet with `hostmetrics`, `kubeletstats`, `filelog/k8s`, and `k8sattributes` enrichment on every pipeline. See [`docs/getting-started/kubernetes.md`](docs/getting-started/kubernetes.md).

### Windows

Download the MSI from the [latest release](https://github.com/conduit-obs/conduit-agent/releases) and run it; the installer registers a Windows Service named `conduit`. See [`docs/getting-started/windows.md`](docs/getting-started/windows.md).

## CLI

```sh
./bin/conduit config --validate -c conduit.yaml   # validates against the schema
./bin/conduit preview            -c conduit.yaml   # renders the upstream OTel Collector YAML
./bin/conduit run                -c conduit.yaml   # boots the embedded collector
./bin/conduit doctor             -c conduit.yaml   # runs the operator-facing check catalog
./bin/conduit --help                                # lists every subcommand
```

The check catalog and per-code fix doc lives at [`docs/troubleshooting/cdt-codes.md`](docs/troubleshooting/cdt-codes.md).

## Documentation

The full documentation set lives under [`docs/`](docs/) — start at [`docs/index.md`](docs/index.md):

- **[Getting started](docs/getting-started/)** — pick a platform: [Linux](docs/getting-started/linux.md), [Docker](docs/getting-started/docker.md), [Kubernetes](docs/getting-started/kubernetes.md), [Windows](docs/getting-started/windows.md). Each guide is self-contained, time-boxed (10–20 minutes), and ends with a `conduit doctor` verification step.
- **[Configuration reference](docs/reference/configuration.md)** — the complete `conduit.yaml` schema with every field, default, and validation rule.
- **[Architecture overview](docs/architecture/overview.md)** — what runs inside the agent, how the expander composes upstream collector YAML, and the lifecycle of a single signal through receivers / processors / connectors / exporters.
- **[Troubleshooting](docs/troubleshooting/)** — first-response cheatsheet, symptom → CDT-code lookup, and the canonical fix doc per check ID.
- **[AWS deployment recipes](docs/deploy/aws/)** — EC2, ECS, EKS, and Lambda guidance.
- **[Release engineering](docs/release/)** — runbook, compatibility matrix, launch checklist.

## Architecture Decision Records

The decisions that lock V0's shape are committed under [`docs/adr/`](docs/adr/). Each ADR captures one decision, its alternatives, and its consequences. Read them in order to understand the build doctrine — pure upstream OTel components ([ADR-0004](docs/adr/adr-0004.md)), `output.mode` rather than per-signal endpoints ([ADR-0008](docs/adr/adr-0008.md)), allowlist-based RED dimensions ([ADR-0006](docs/adr/adr-0006.md)), `conduit.yaml` expands to upstream YAML with `overrides:` as the only escape hatch ([ADR-0012](docs/adr/adr-0012.md)), and so on. New decisions get a new ADR.

## Phase scope

| Phase | Theme | Headline | Out of scope |
|---|---|---|---|
| **V0** | Adoption bridge (agent only) | OCB-based distribution; Linux / Windows / Docker / K8s Helm; AWS recipes; declarative `output:` block (`honeycomb` or `gateway`, with optional Refinery for traces); RED metrics before sampling; `conduit doctor` and `conduit preview`; safe-by-default cardinality and redaction | Conduit-as-gateway; agent-side fanout; remote config; fleet inventory; Lambda extension |
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
```

See [Makefile](Makefile) for the full target list.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). The two rules engineers need to know on day one:

1. **Clean-room implementation.** Conduit is written from scratch — no verbatim code copy from any other agent or distribution. Patterns may be borrowed conceptually with attribution.
2. **No custom OTel processors or receivers in V0.** Pure upstream only ([ADR-0004](docs/adr/adr-0004.md)).

## Security

Report vulnerabilities to `security@conduit-obs.com`. See [`SECURITY.md`](SECURITY.md) for the disclosure process.

## License

[Apache-2.0](LICENSE). Copyright 2026 The Conduit Authors.
