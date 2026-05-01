# deploy/helm

Kubernetes Helm chart for the Conduit agent.

| Path | Status | Purpose |
|---|---|---|
| [`conduit-agent/`](conduit-agent/) | M5.A — skeleton | Chart skeleton: DaemonSet, ConfigMap, ServiceAccount, Service, optional Secret. Default `profile.mode: k8s` (OTLP-only on `0.0.0.0`). |

## Slice plan (M5)

| Slice | Status | Adds |
|---|---|---|
| **M5.A** | done | `ProfileModeK8s` schema knob, expander binding to `0.0.0.0`, chart skeleton (above), `helm install` works as an OTLP relay. |
| **M5.B** | pending | `internal/profiles/k8s/{hostmetrics,kubelet,logs}.yaml` fragments — kubelet stats, container log filelog, `k8sattributes` enrichment + tests. |
| **M5.C** | pending | ClusterRole + ClusterRoleBinding for the M5.B receivers; DaemonSet host mounts (`/var/log`, `/var/lib/docker/containers`); kind smoke recipe. |
| **M5.D** | pending | Goreleaser publishing of the chart to `oci://ghcr.io/conduit-obs/charts/conduit-agent` (registry venue per [ADR-0019](../../docs/adr/adr-0019.md); resolves OQ-2). |
| **M5.E** | pending | `dashboards/k8s-cluster-overview.json` — pod-keyed, namespace-scoped board satisfying [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) §3. |

See [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) for the platform profile contract M5.B+ delivers against.
