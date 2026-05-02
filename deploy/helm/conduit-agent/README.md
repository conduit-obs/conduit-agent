# conduit-agent Helm chart

DaemonSet-based deployment of the Conduit OpenTelemetry agent on Kubernetes.

> **Status — M5.A/B/C complete.** The chart ships the DaemonSet, ConfigMap,
> ServiceAccount, Service, optional Secret, and ClusterRole +
> ClusterRoleBinding. Default `profile.mode: k8s` ships per-node
> `hostmetrics` (rooted at `/hostfs` via the chart bind mount),
> `kubeletstats` against the local kubelet, `filelog/k8s` for
> `/var/log/pods/*`, and the `k8sattributes` processor on every pipeline.
> Chart publishing to `oci://ghcr.io/conduit-obs/charts/conduit-agent`
> lands in M5.D; the default cluster + workload boards in M5.E. A kind
> smoke recipe lives in the repo root `Makefile` (`make kind-smoketest`).

## What you get

- A DaemonSet, one pod per `kubernetes.io/os=linux` node, running
  `ghcr.io/conduit-obs/conduit-agent`.
- OTLP receivers on `0.0.0.0:4317` (gRPC) and `0.0.0.0:4318` (HTTP) inside
  each pod (per `profile.mode: k8s`).
- A ClusterIP `Service` exposing those ports cluster-wide so peer pods send
  to `<release>-conduit-agent:4317` / `:4318`.
- A health-check endpoint on every pod at `:13133` for liveness / readiness
  probes.
- Per-node host metrics + per-pod / per-container kubelet stats +
  container logs spliced into the matching pipelines, with every signal
  enriched with Kubernetes workload metadata (`k8s.namespace.name`,
  `k8s.pod.name`, `k8s.deployment.name`, ...).
- A `ServiceAccount` + read-only `ClusterRole` (pods, namespaces, nodes,
  apps + batch workload kinds) bound by a `ClusterRoleBinding`. Disable
  with `rbac.create=false` if your cluster manages RBAC out-of-band.
- Host bind mounts that the M5.B receivers need: `/hostfs` (read-only,
  `mountPropagation: HostToContainer`), `/var/log/pods`,
  `/var/log/containers`. Disable with `daemonset.hostMounts.enabled=false`
  for an OTLP-only relay (also set `conduit.profileMode=none`).

## Install (local source)

The chart is not yet published. Install from a clone:

```bash
git clone https://github.com/conduit-obs/conduit-agent.git
cd conduit-agent

# 1. Pre-create the API-key Secret (recommended).
kubectl create namespace conduit
kubectl -n conduit create secret generic conduit-honeycomb \
  --from-literal=HONEYCOMB_API_KEY=hcaik_...

# 2. Install the chart.
helm install conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=edge-cluster-prod \
  --set honeycomb.existingSecret=conduit-honeycomb
```

Verify the rollout:

```bash
kubectl -n conduit rollout status ds/conduit-conduit-agent
kubectl -n conduit get pods -l app.kubernetes.io/name=conduit-agent
```

Send a smoke trace from any pod in the cluster:

```bash
curl -X POST http://conduit-conduit-agent.conduit:4318/v1/traces \
  -H 'Content-Type: application/json' \
  -d '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"smoketest"}}]},"scopeSpans":[{"spans":[{"traceId":"01020304050607080102030405060708","spanId":"0102030405060708","name":"smoke","startTimeUnixNano":1,"endTimeUnixNano":2}]}]}]}'
```

## Install (post-M5.D — OCI)

Once M5.D wires goreleaser to push the chart, the steady-state install
recipe will be:

```bash
helm install conduit oci://ghcr.io/conduit-obs/charts/conduit-agent \
  --version 0.1.x \
  --namespace conduit --create-namespace \
  --set conduit.serviceName=edge-cluster-prod \
  --set honeycomb.existingSecret=conduit-honeycomb
```

## Values reference

The full annotated reference is `values.yaml`. The most-used knobs:

| Key | Default | Purpose |
|---|---|---|
| `conduit.serviceName` | _required_ | `service.name` resource attribute on every signal. |
| `conduit.deploymentEnvironment` | `production` | `deployment.environment` resource attribute. |
| `conduit.profileMode` | `k8s` | Profile mode (`auto` / `linux` / `darwin` / `docker` / `k8s` / `none`). |
| `honeycomb.apiKey` | `""` | Plain-text API key. Convenient for smoke tests; **prefer `existingSecret` in real environments.** |
| `honeycomb.existingSecret` | `""` | Pre-created Secret holding `HONEYCOMB_API_KEY`. Wins over `apiKey`. |
| `honeycomb.endpoint` | `https://api.honeycomb.io` | Switch to `https://api.eu1.honeycomb.io` for the EU region. |
| `gateway.enabled` | `false` | Set true to route via a customer-operated OTLP gateway instead of Honeycomb-direct. |
| `gateway.endpoint` | `""` | OTLP/gRPC URL of the gateway. Required when `gateway.enabled=true`. |
| `image.repository` | `ghcr.io/conduit-obs/conduit-agent` | OCI image (per ADR-0019). |
| `image.tag` | `""` (falls back to `Chart.appVersion`) | Pin a specific agent build. |
| `daemonset.resources` | `requests: 50m / 96Mi`, `limits: 500m / 384Mi` | Sized for the M5.B receiver set + k8sattributes. Bump if memory_limiter starts dropping batches. |
| `daemonset.tolerations` | `[{operator: Exists}]` | Wide-open by default so the agent runs on system / GPU nodes too. Tighten for high-security clusters. |
| `daemonset.hostMounts.enabled` | `true` | Mount `/hostfs` (the host root, read-only, with `HostToContainer` propagation) plus `/var/log/pods` and `/var/log/containers` so hostmetrics + filelog/k8s see the node. Disable for an OTLP-only relay (also flip `conduit.profileMode` to `none`). |
| `daemonset.runAsRoot` | `true` | Run the conduit container as UID 0. Required by the default profile because kubelet log directories are mode 0700 root-owned and `/proc/<pid>` for non-root pods isn't readable as a different unprivileged UID. Flip to `false` together with `profileMode=none`/`docker` to fall back to the image's baked-in nonroot UID 65532. |
| `rbac.create` | `true` | Create the ClusterRole + ClusterRoleBinding the M5.B receivers and processor need (read-only access to pods / namespaces / nodes / apps + batch workload kinds). Disable when your cluster manages RBAC out-of-band; review `templates/clusterrole.yaml` to audit the exact rule set. |
| `serviceAccount.create` | `true` | Set false to bind the DaemonSet to an external SA. |
| `service.enabled` | `true` | Cluster-internal Service for OTLP ingress. |

## Output mode

The chart ships two egress modes, controlled by `gateway.enabled`:

- **Honeycomb-direct (default).** OTLP/HTTP to
  `https://api.honeycomb.io`. The `HONEYCOMB_API_KEY` env var is wired into
  the DaemonSet via `secretKeyRef`, so the chart-rendered `conduit.yaml`
  never has the literal key embedded.
- **Gateway.** OTLP/gRPC to a customer-operated gateway (any OTLP-capable
  collector, including the Honeycomb Collector). Set `gateway.enabled: true`
  and `gateway.endpoint`. TLS is required by default (ADR-0009);
  `gateway.headers` is the place to put any gateway-specific auth.

## OTLP bind address (`0.0.0.0` vs `127.0.0.1`)

`profile.mode: k8s` tells the conduit expander to bind the OTLP receivers
to `0.0.0.0` so peer pods can reach them through the Service. Host-mode
profiles (`linux`, `darwin`, `none`) bind to `127.0.0.1` so a stock host
install never silently exposes OTLP to the LAN. See
[`internal/expander/expander.go`](../../../internal/expander/expander.go)
`resolveOTLPBindAddress`.

## Lint and template

```bash
# From the repo root.
helm lint deploy/helm/conduit-agent

# Render templates locally to inspect what will be applied.
helm template conduit deploy/helm/conduit-agent \
  --namespace conduit \
  --set conduit.serviceName=smoketest \
  --set honeycomb.apiKey=hcaik_dummy
```

## kind smoke test

Repo root has `make kind-smoketest`, which spins up a disposable kind
cluster (`conduit-smoke`), builds the agent image from the source
Dockerfile, loads it into the cluster, helm-installs the chart with a
dummy Honeycomb key, sends a test trace through the cluster Service, and
greps the conduit pod logs for the trace before exiting. Use it to
verify chart changes end-to-end without a real Honeycomb tenant. Tear
down with `make kind-down`.

## Related docs

- [`docs/adr/adr-0019.md`](../../../docs/adr/adr-0019.md) — registry venue (`ghcr.io/conduit-obs/...`).
- [`internal/profiles/PROFILE_SPEC.md`](../../../internal/profiles/PROFILE_SPEC.md) — the platform profile contract M5.B+ delivers against.
- [`deploy/docker/README.md`](../../docker/README.md) — sibling Docker deployment path.
