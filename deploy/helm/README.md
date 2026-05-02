# deploy/helm

Kubernetes Helm chart for the Conduit agent.

| Path | Status | Purpose |
|---|---|---|
| [`conduit-agent/`](conduit-agent/) | M5.A/B/C — receivers + RBAC + host mounts | DaemonSet, ConfigMap, ServiceAccount, Service, optional Secret, ClusterRole + ClusterRoleBinding. Default `profile.mode: k8s` ships per-node `hostmetrics` (`root_path: /hostfs`), `kubeletstats`, `filelog/k8s`, and `k8sattributes` enrichment on every pipeline. |

## Slice plan (M5)

| Slice | Status | Adds |
|---|---|---|
| **M5.A** | done | `ProfileModeK8s` schema knob, expander binding to `0.0.0.0`, chart skeleton (above), `helm install` works as an OTLP relay. |
| **M5.B** | done | `internal/profiles/k8s/{hostmetrics,kubelet,logs}.yaml` + `k8sattributes` processor — kubelet stats, container log filelog, k8s metadata enrichment on every pipeline. See [`internal/profiles/k8s/README.md`](../../internal/profiles/k8s/README.md). |
| **M5.C** | done | ClusterRole + ClusterRoleBinding (read-only access to pods / namespaces / nodes / apps + batch workload kinds) gated by `rbac.create`; DaemonSet host bind mounts (`/hostfs` with `HostToContainer` propagation, `/var/log/pods`, `/var/log/containers`) gated by `daemonset.hostMounts.enabled`; runAsRoot security context; `make kind-smoketest` recipe in the repo root `Makefile`. |
| **M5.D** | pending | Goreleaser publishing of the chart to `oci://ghcr.io/conduit-obs/charts/conduit-agent` (registry venue per [ADR-0019](../../docs/adr/adr-0019.md); resolves OQ-2). |
| **M5.E** | pending | `dashboards/k8s-cluster-overview.json` — pod-keyed, namespace-scoped board satisfying [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) §3. |

See [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) for the platform profile contract M5.B+ delivers against.
