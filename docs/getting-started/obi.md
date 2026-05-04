# Getting started — OBI (zero-code application instrumentation)

**Time**: ~10 minutes on a Linux host that already has Conduit installed.

[OpenTelemetry eBPF Instrumentation (OBI)](https://opentelemetry.io/docs/zero-code/obi/)
is the upstream OTel project that captures HTTP / gRPC / database RED
metrics and distributed trace spans **without touching application
code**. Conduit V0.1 integrates OBI as a Collector receiver per
[ADR-0020](../adr/adr-0020.md): one YAML flag turns it on, and the
embedded collector handles the eBPF plumbing.

This guide covers turning OBI on for an existing Conduit install. If
you haven't installed Conduit yet, start with the platform guide
([linux](linux.md) or [kubernetes](kubernetes.md)) and circle back.

## When to use OBI

OBI is the right tool when you have **services you can't (or won't)
instrument with an OTel SDK** — third-party binaries, legacy services,
multi-language fleets where adding SDK overhead per language is
painful. It captures spans and RED metrics for HTTP / gRPC / database
clients automatically by attaching eBPF probes to the running process.

It's **not** a replacement for SDK instrumentation when you have one.
SDK traces carry richer context (custom attributes, business spans,
parent-child relationships your code knows about); OBI gives you the
network-protocol-level view of what every process is doing. The two
are complementary, and Conduit emits both alongside each other when
both are present.

## Constraints up front

- **Linux only** — kernel ≥ 5.8 (mainline) or ≥ 4.18 (RHEL-family
  with backports). Ubuntu 18.04 and RHEL 7 cannot run OBI.
- **Privilege grant required** — OBI needs `CAP_SYS_ADMIN`,
  `CAP_DAC_READ_SEARCH`, `CAP_NET_RAW`, `CAP_SYS_PTRACE`,
  `CAP_PERFMON`, `CAP_BPF`. The Conduit installer / Helm chart
  grants these only when you opt in (`--with-obi` / `obi.enabled:
  true`), never automatically.
- **Build pipeline (V0.1 caveat)** — adding OBI to the Conduit OCB
  manifest requires a non-trivial build step (the upstream Go module
  ships without pre-generated eBPF bindings). The schema, expander,
  doctor preflight, install script, and Helm chart all land in V0.1
  with the OBI surface ready; the OCB manifest line and a
  prebuilt-with-OBI binary follow once the build pipeline is decided
  (see [ADR-0020 § Open question: build pipeline](../adr/adr-0020.md)).
  Until then, `obi.enabled: true` produces a fast, clear
  `conduit doctor` failure (`CDT0204 — receiver.obi`) instead of a
  silent startup error. Track [issue TBD](#) for build availability.

## Linux host

### 1. Re-run the installer with `--with-obi`

The installer writes a systemd drop-in that grants the eBPF
capabilities and reloads the unit. It's idempotent — running it again
regenerates the file rather than appending.

```sh
curl -fsSL https://github.com/conduit-obs/conduit-agent/releases/latest/download/install_linux.sh \
  | sudo bash -s -- --with-obi
```

This writes `/etc/systemd/system/conduit.service.d/obi.conf` with
`CapabilityBoundingSet=` + `AmbientCapabilities=` granting the OBI
caps.

### 2. Turn OBI on in `conduit.yaml`

Edit `/etc/conduit/conduit.yaml` and add:

```yaml
obi:
  enabled: true
```

That's it. OBI's auto-discovery picks up every process on the host;
emitted metrics carry `instrumentation.scope = "obi"` so you can
filter them at query time. Operators who want OBI to be the **only**
RED source (suppressing the M8 `span_metrics` connector) add:

```yaml
obi:
  enabled: true
  replace_span_metrics_connector: true
```

### 3. Restart and verify

```sh
sudo systemctl restart conduit
sudo conduit doctor -c /etc/conduit/conduit.yaml
```

`conduit doctor` runs the OBI preflight (`CDT0204 — receiver.obi`)
which checks: kernel version, BTF availability at
`/sys/kernel/btf/vmlinux`, every required cap on the running process,
and that the binary actually has the OBI receiver linked. Any failure
is a single line of remediation; the
[CDT0204 troubleshooting page](../troubleshooting/cdt-codes.md#cdt0204--receiver-obi)
covers each failure mode.

### 4. Send some traffic

OBI doesn't need any work on the application side. Generate some
HTTP traffic against any service running on the host (curl, postman,
your own load test) and check Honeycomb / your destination — you
should see spans with `instrumentation.scope = "obi"` arriving with
`http.method`, `http.route`, and `http.status_code` set.

To turn OBI back off, set `obi.enabled: false` in `conduit.yaml`,
restart conduit, and remove the drop-in:

```sh
sudo rm /etc/systemd/system/conduit.service.d/obi.conf
sudo systemctl daemon-reload
sudo systemctl restart conduit
```

## Kubernetes

### 1. Set `obi.enabled` in your Helm values

```yaml
# values.yaml
obi:
  enabled: true
  # Optional: make OBI the sole RED source by suppressing
  # span_metrics for spans that already pass through the agent.
  # replaceSpanMetricsConnector: true
```

Then `helm upgrade`:

```sh
helm upgrade conduit \
  oci://ghcr.io/conduit-obs/charts/conduit-agent \
  --version 0.x.y \
  --namespace conduit \
  --reuse-values \
  --set obi.enabled=true
```

The chart adds the OBI capability set to the daemonset's
`securityContext.capabilities.add`, forces `hostPID: true` (OBI needs
to attach probes to processes by host PID), and writes
`obi: { enabled: true }` into the rendered `conduit.yaml`. No other
chart values change.

### 2. Verify

```sh
kubectl -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=200
kubectl -n conduit exec -i ds/conduit-conduit-agent -- \
  conduit doctor -c /etc/conduit/conduit.yaml
```

The doctor check inside the pod is the same `CDT0204 — receiver.obi`
preflight as on Linux hosts. On a healthy install all five sub-checks
PASS.

### 3. Auto-attach to all pods

Once OBI is running, it auto-discovers processes across every container
on the node (the daemonset's `hostPID: true` gives it the host-wide
view). On the k8s profile, the rendered config carries
`obi.attributes.kubernetes.enable: true`, so every emitted span /
metric is automatically tagged with `k8s.namespace.name`,
`k8s.pod.name`, `k8s.deployment.name`, and `k8s.node.name` — matching
what `k8sattributes` does for OTLP traffic.

## Tuning beyond the curated schema

The Conduit `obi:` block is intentionally minimal — `enabled` and
`replace_span_metrics_connector`, nothing else. Any other OBI knob
(per-port instrumentation filters, feature toggles, discovery poll
interval, k8s metadata extraction tuning) goes through the
[`overrides:` escape hatch](../adr/adr-0012.md):

```yaml
obi:
  enabled: true

overrides:
  receivers:
    obi:
      meter_provider:
        # Add network features to the default {application} set.
        features: [application, network]
      discovery:
        poll_interval: 60s
      instrument:
        - service_name: web-app
        - service_name: api-gateway
```

The embedded collector deep-merges `overrides:` over Conduit's
rendered YAML at startup. Operators reaching for `overrides:` for OBI
heavily is a signal the curated schema needs to grow — file an issue
and we'll consider promoting the field per ADR-0012's review cadence.

## Reference

- [ADR-0020](../adr/adr-0020.md) — design rationale.
- [Upstream OBI documentation](https://opentelemetry.io/docs/zero-code/obi/).
- [Upstream OBI Collector receiver mode](https://opentelemetry.io/docs/zero-code/obi/configure/collector-receiver/).
- [`CDT0204 — receiver.obi`](../troubleshooting/cdt-codes.md#cdt0204--receiver-obi)
  troubleshooting guide.
