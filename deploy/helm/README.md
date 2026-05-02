# deploy/helm

Kubernetes Helm chart for the Conduit agent.

| Path | Status | Purpose |
|---|---|---|
| [`conduit-agent/`](conduit-agent/) | M5.A/B/C/D — receivers + RBAC + host mounts + publishing | DaemonSet, ConfigMap, ServiceAccount, Service, optional Secret, ClusterRole + ClusterRoleBinding. Default `profile.mode: k8s` ships per-node `hostmetrics` (`root_path: /hostfs`), `kubeletstats`, `filelog/k8s`, and `k8sattributes` enrichment on every pipeline. Published as a signed OCI chart at `oci://ghcr.io/conduit-obs/charts/conduit-agent` once a release tag fires `make helm-publish`. |

## Slice plan (M5)

| Slice | Status | Adds |
|---|---|---|
| **M5.A** | done | `ProfileModeK8s` schema knob, expander binding to `0.0.0.0`, chart skeleton (above), `helm install` works as an OTLP relay. |
| **M5.B** | done | `internal/profiles/k8s/{hostmetrics,kubelet,logs}.yaml` + `k8sattributes` processor — kubelet stats, container log filelog, k8s metadata enrichment on every pipeline. See [`internal/profiles/k8s/README.md`](../../internal/profiles/k8s/README.md). |
| **M5.C** | done | ClusterRole + ClusterRoleBinding (read-only access to pods / namespaces / nodes / apps + batch workload kinds) gated by `rbac.create`; DaemonSet host bind mounts (`/hostfs` with `HostToContainer` propagation, `/var/log/pods`, `/var/log/containers`) gated by `daemonset.hostMounts.enabled`; runAsRoot security context; `make kind-smoketest` recipe in the repo root `Makefile`. |
| **M5.D** | done | Chart packaging + OCI publishing recipe in the repo-root `Makefile` (`make helm-package` / `make helm-publish` — the latter does lint + package + push + cosign sign). Targets `oci://ghcr.io/conduit-obs/charts/conduit-agent` per [ADR-0019](../../docs/adr/adr-0019.md); cosign keyless OIDC by default, `COSIGN_KEY=<path>` for local key signing. CI hook lands in M12; the recipe is the contract. |
| **M5.E** | done | [`dashboards/k8s-cluster-overview.json`](../../dashboards/k8s-cluster-overview.json) — pod-keyed, namespace-scoped board satisfying [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) §3 (Cluster shape → Compute pressure absolute → Compute pressure relative to limits → Network → Filesystem → Logs); ships alongside an additive opt-in metric set in [`internal/profiles/k8s/kubelet.yaml`](../../internal/profiles/k8s/kubelet.yaml) (`container.uptime`, `k8s.pod.{cpu,memory}_limit_utilization`) so the board's columns are actually emitted by the rendered pipeline. |

See [`internal/profiles/PROFILE_SPEC.md`](../../internal/profiles/PROFILE_SPEC.md) for the platform profile contract M5.B+ delivers against.
