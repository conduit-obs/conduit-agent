# profiles/k8s

The Kubernetes profile (M5). Conduit's per-node DaemonSet shape: the agent
runs as a `Pod` on every Linux node, scrapes that node's host metrics
through bind-mounted `/proc` and `/sys`, scrapes the local kubelet for
per-pod metrics, tails container logs, and accepts OTLP from peer apps in
the cluster — then forwards everything to Honeycomb (or a customer
gateway).

## What this directory ships

| Fragment | Receiver(s) | Pipeline | Notes |
|---|---|---|---|
| [`hostmetrics.yaml`](hostmetrics.yaml) | `hostmetrics` | metrics | Same scraper set as the Linux profile (CPU / memory / load / disk / filesystem / network / paging / processes), with `root_path: /hostfs` so the scrapers read `/proc` and `/sys` from the chart-provided host bind mount instead of the pod's own filesystem. Reports per-node stats keyed on `host.name`. |
| [`kubelet.yaml`](kubelet.yaml) | `kubeletstats` | metrics | Talks to the local kubelet on `:10250` using the pod's ServiceAccount. Scopes to the local node via `K8S_NODE_NAME` (set by the chart's DaemonSet from the downward API) so each Conduit pod scrapes only the kubelet on the same node. Emits per-node, per-pod, and per-container CPU + memory + network + filesystem (defaults), plus the small opt-in set called out below. |
| [`logs.yaml`](logs.yaml) | `filelog/k8s` | logs | Tails `/var/log/pods/*/*/*.log` (the kubelet-managed path layout) with the upstream `container` operator that auto-detects CRI / containerd / Docker JSON formats. Excludes Conduit's own pods to avoid feedback loops. |

The matching processor — `k8sattributes` — is *not* a fragment. It lives
in the base template
([`internal/expander/templates/base.yaml.tmpl`](../../expander/templates/base.yaml.tmpl)),
gated by a `K8sAttributes` flag that the expander sets when
`profile.mode=k8s`. It runs on every pipeline (traces / metrics / logs)
so OTLP signals arriving from instrumented apps in the cluster get the
same Kubernetes workload metadata that the chart-shipped receivers do.

## Opt-in kubeletstats metrics Conduit enables

The kubeletstats receiver ships with a default-enabled set covering raw
CPU / memory / network / filesystem in absolute units (cores, bytes,
bytes/s). For the M5.E default board to answer the questions Datadog /
Grafana operators expect a k8s overview to answer, Conduit also enables
a small opt-in set:

| Metric | Why we enable it |
|---|---|
| `container.uptime` | Without `k8sclusterreceiver` (which we don't ship in V0) there's no `k8s.container.restarts` counter. `container.uptime` dropping to ~0 is the closest restart proxy and powers the **Container Uptime by Pod** panel. |
| `k8s.pod.uptime` | Same idea at the pod level; useful for pod-restart investigations. |
| `k8s.pod.cpu_limit_utilization` | "% of CPU limit" — the right answer to "is this pod hot?" when a limit is set. >1 = the pod is being CPU-throttled by the kubelet. Only emits for pods with limits, which is itself useful capacity-review signal. |
| `k8s.pod.memory_limit_utilization` | "% of memory limit" — approaching 1.0 means OOM-kill is imminent. Pods without limits are absent (intentionally — without a limit the metric isn't meaningful). |

We deliberately keep this set small. `k8s.{pod,container}.{cpu,memory}.node.utilization` (% of node capacity) and `k8s.pod.cpu_request_utilization` etc. are upstream-emitted and useful in specific contexts but we leave them off by default to keep cardinality predictable; operators can flip them on via `overrides:` in conduit.yaml. See [ADR-0012](../../../docs/adr/adr-0012.md) for the override mechanism.

## Why kubelet is bundled with hostmetrics

There is no useful Kubernetes metrics story without both halves:
host-level scraping (CPU pressure, disk fill, network errors)
contextualized against pod-level scraping (which workload is causing
that pressure). The `host_metrics` toggle in `conduit.yaml` covers both;
operators who want only one should use `overrides:` to surgically
disable a receiver. See `loadProfileFragments` in
[`internal/expander/expander.go`](../../expander/expander.go).

## Authoring contract

The two rules from [`internal/profiles/README.md`](../README.md) apply:
top-level keys are receiver instance IDs; no top-level wrapping. The k8s
loader follows the same `(platform, signal)` lookup as the host
profiles — adding a fourth fragment kind would mean a new
`profiles.SignalXxx` constant and a new file under this directory.

## Profile contract status

[`PROFILE_SPEC.md`](../PROFILE_SPEC.md) §1 ("Telemetry the profile MUST
emit") for k8s:

| Section | Status |
|---|---|
| Resource attributes | `host.name`, `os.type`, etc. provided by `resourcedetectionprocessor`; `k8s.namespace.name` / `k8s.pod.name` / `k8s.deployment.name` / labels added by `k8sattributes` on every signal; `k8s.container.name` / `k8s.pod.uid` extracted from the container-log filepath by the `container` operator. |
| Host metrics | Per-node CPU / memory / load / disk / filesystem / network / paging / processes via `hostmetrics` (re-rooted at `/hostfs` per the chart bind mount) plus per-pod CPU + memory via `kubeletstats`. |
| Logs | Container logs from every pod on the node via `filelog/k8s`; system logs from the node itself stay deferred to M9 (the DaemonSet would need `/var/log/syslog` mounted in, and operators with structured node logging usually pipe it through OTLP already). |

The dashboard quality bar (`PROFILE_SPEC.md` §3) is satisfied by
[`dashboards/k8s-cluster-overview.json`](../../../dashboards/k8s-cluster-overview.json) (M5.E):
a k8s-native opinionated board keyed off `k8s.pod.name` /
`k8s.namespace.name` / `k8s.node.name`, narrative organized around
the questions an SRE actually asks of a cluster (Cluster shape →
Compute pressure absolute → Compute pressure relative to limits →
Network → Filesystem → Logs) — not a copy of the host-overview
skeleton. A future `dashboards/k8s-workload-overview.json` will key
off `k8s.deployment.name` / `k8s.daemonset.name` /
`k8s.statefulset.name` for workload-scoped drill-down.

## See also

- [`deploy/helm/conduit-agent/`](../../../deploy/helm/conduit-agent/) —
  the chart that runs this profile, owns the `/hostfs` and
  `/var/log/{pods,containers}` host bind mounts, and grants the
  read-only `ClusterRole` kubeletstats + k8sattributes need.
- [`internal/expander/expander.go`](../../expander/expander.go)
  §`profileWantsK8sAttributes` for the rule that ties the processor to
  `profile.mode=k8s`.
- [`internal/profiles/PROFILE_SPEC.md`](../PROFILE_SPEC.md) — the
  cross-platform contract this profile must satisfy.
