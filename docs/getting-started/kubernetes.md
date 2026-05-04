# Getting started — Kubernetes

**Time to first signal**: ~20 minutes on an existing cluster. This
guide takes you from "I have a kubectl context" to "every node's
host metrics, kubelet metrics, container logs, and OTLP traffic from
every pod are landing in Honeycomb."

## What you'll have at the end

- A `conduit-agent` DaemonSet running one pod per node in the
  `conduit` namespace.
- Per-node host metrics (`hostmetrics`) — CPU, memory, filesystem,
  network — keyed by `host.name`.
- Per-node kubelet metrics (`kubeletstats`) — pod CPU, pod memory,
  container CPU / memory limit utilization — keyed by
  `k8s.namespace.name`, `k8s.pod.name`, `k8s.container.name`.
- Container logs (`filelog/k8s` reading `/var/log/pods/*`) with the
  upstream `container` operator parsing the cri-o / containerd format.
- Every signal enriched with k8s metadata via the `k8sattributes`
  processor (`k8s.deployment.name`, `k8s.daemonset.name`, …).
- An OTLP receiver bound to `0.0.0.0:4317` / `:4318` inside each agent
  pod, exposed via a per-namespace `Service` so app pods can send
  traces to `conduit-agent.conduit.svc:4317`.

## Prerequisites (5 min)

| Item | Where to get it |
|---|---|
| `kubectl` against a Kubernetes 1.27+ cluster | EKS, GKE, AKS, kind, k3d, anything OTel-supported |
| `helm` 3.13+ | [helm.sh/docs/intro/install](https://helm.sh/docs/intro/install/) |
| `cluster-admin` permissions on the target cluster (the chart installs a `ClusterRole` for `k8sattributes`) | — |
| A Honeycomb ingest API key | [honeycomb.io](https://www.honeycomb.io) → API Keys |

For EKS specifically, see the [AWS EKS recipe](../deploy/aws/eks.md)
— it covers IRSA so the API key can come from Secrets Manager / SSM
Parameter Store instead of being inlined.

## Step 1 — Install the chart (5 min)

The chart is published to GHCR as an OCI artifact. Pull-and-install:

```sh
# Replace 0.x.y with the latest chart version.
helm install conduit \
  oci://ghcr.io/conduit-obs/charts/conduit-agent \
  --version 0.x.y \
  --namespace conduit --create-namespace \
  --set conduit.serviceName=cluster-prod \
  --set conduit.deploymentEnvironment=production \
  --set honeycomb.apiKey="$HONEYCOMB_API_KEY"
```

Wait for the DaemonSet to roll out:

```sh
kubectl -n conduit rollout status ds/conduit-conduit-agent --timeout=120s
```

That's it. The chart:

1. Creates the `conduit` namespace.
2. Installs a DaemonSet that runs one agent pod per node.
3. Creates a `ClusterRole` + `ClusterRoleBinding` granting `get` /
   `list` on `pods`, `nodes`, `namespaces` (the verbs the
   `k8sattributes` processor needs).
4. Mounts `/proc`, `/sys`, `/`, and `/var/log/pods` from the host
   into the agent pod so the host-metric and pod-log receivers see
   the right data.
5. Exposes a `ClusterIP` Service named `<release>-conduit-agent`
   on `4317` (gRPC) + `4318` (HTTP) so app pods can reach the agent.
6. Stores the API key in a Kubernetes Secret;
   `conduit.yaml` references it via `${env:HONEYCOMB_API_KEY}`.

For chart internals (`values.yaml`, RBAC, host bind mounts), see
[`deploy/helm/conduit-agent/`](../../deploy/helm/conduit-agent/README.md).

> **Optional: zero-code application instrumentation (OBI).** If you
> want HTTP / gRPC / database RED metrics + traces from every service
> on every node *without* adding an OTel SDK to your apps, set
> `obi.enabled=true` in the chart values. The chart adds the eBPF
> capability set and `hostPID: true` to the daemonset, and the
> rendered `conduit.yaml` activates the OBI receiver. See the
> [OBI guide](obi.md) for the full walkthrough; see
> [ADR-0020](../adr/adr-0020.md) for why it's off by default in V0.1
> (the build pipeline that links OBI into the binary is staged behind
> a follow-up decision).

### Pinning the chart version

Don't use `latest` in production — `helm pull` the chart into your
GitOps repo and pin the version:

```sh
helm pull oci://ghcr.io/conduit-obs/charts/conduit-agent --version 0.x.y
# Commit conduit-agent-0.x.y.tgz to your GitOps repo.
```

The chart is signed with cosign keyless OIDC — verify before
pulling:

```sh
cosign verify-blob \
  --certificate-identity-regexp 'https://github.com/conduit-obs/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature conduit-agent-0.x.y.tgz.sig \
  --certificate conduit-agent-0.x.y.tgz.pem \
  conduit-agent-0.x.y.tgz
```

## Step 2 — Verify (5 min)

Confirm every node has a pod and they're all `Ready`:

```sh
kubectl -n conduit get pods -o wide
```

Tail one agent's logs:

```sh
kubectl -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=50 \
  | head -80
```

Look for the embedded collector's `Everything is ready. Begin
running and processing data.` line.

Run the doctor inside an agent pod:

```sh
POD=$(kubectl -n conduit get pod -l app.kubernetes.io/name=conduit-agent -o name | head -1)
kubectl -n conduit exec $POD -- /conduit doctor -c /etc/conduit/conduit.yaml
```

The k8s profile binds OTLP to `0.0.0.0` so `receiver.ports` checks
`0.0.0.0:4317` / `:4318`. CDT0301 (`k8s.permissions`) is reserved
for now — it'll surface RBAC drift once the check ships; today, the
`k8sattributes` processor would log auth errors at startup if the
ClusterRoleBinding was missing.

## Step 3 — Confirm data in Honeycomb (5 min)

Open the dataset matching `conduit.serviceName` (here:
`cluster-prod`):

| Where to look | What you'll see |
|---|---|
| **Datasets list** | `cluster-prod` |
| **Query** → metric: `system.cpu.utilization`, group by `host.name` | One row per node |
| **Query** → metric: `k8s.pod.cpu.utilization`, group by `k8s.namespace.name` | Per-namespace pod CPU |
| **Query** → group by `k8s.container.name`, `severity_text` | Container logs by severity |

The shipped [`dashboards/k8s-cluster-overview.json`](../../dashboards/k8s-cluster-overview.json)
gives you a six-section narrative (cluster shape → compute absolute
→ compute relative to limits → network → filesystem → logs) on a
single board — import it via Honeycomb's `Boards → Import` and pick
your dataset.

## Step 4 — Send traces from app pods

The chart exposes a `ClusterIP` Service. Point your app pods'
OTel SDKs at it:

```yaml
# In your app Deployment's pod spec:
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://conduit-conduit-agent.conduit.svc:4318
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: service.name=checkout,deployment.environment=production
```

The agent injects the Honeycomb API key on egress; app pods never
see the key, even if they crash-loop with the env vars in the dump.

For `k8sattributes` enrichment to work, the OTel SDK should also
emit the standard k8s downward-API attributes:

```yaml
env:
  - name: K8S_NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
  - name: K8S_POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
  - name: K8S_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: >-
      service.name=checkout,
      k8s.node.name=$(K8S_NODE_NAME),
      k8s.pod.name=$(K8S_POD_NAME),
      k8s.namespace.name=$(K8S_NAMESPACE)
```

The processor does pod IP / UID / connection-based association, so
even apps that don't emit the downward-API attrs still get enriched
— this is the belt-and-suspenders pattern.

## Step 5 — Switch output mode (optional)

Same one-config-field shape as the host install. Update your Helm
values:

```yaml
# values.override.yaml
output:
  mode: gateway
  gateway:
    endpoint: gateway.observability.svc:4317
```

```sh
helm upgrade conduit oci://ghcr.io/conduit-obs/charts/conduit-agent \
  --version 0.x.y \
  --namespace conduit \
  -f values.override.yaml
```

The chart's rolling-upgrade strategy means each agent pod restarts
in turn; total disruption window is ~30 seconds per node.

## Troubleshooting

### CDT0202 receiver.permissions on a kubelet log path

The agent runs as the chart's `runAsRoot: true` security context so
it can read `/var/log/pods/`. If your security policy disallows
running as root, edit the values file:

```yaml
daemonset:
  securityContext:
    runAsUser: 65532
    runAsNonRoot: true
```

You'll then need a privileged init container that `chgrp`s the log
directories to the agent's primary group. The chart doesn't ship
this pattern by default — open an issue if you need it documented.

### `kubeletstats` reports 401 / 403 errors

The kubelet API requires authentication; the chart authenticates via
the agent pod's ServiceAccount token + the `auth_type:
serviceAccount` knob in the kubelet receiver fragment.

If you see auth errors, confirm the ClusterRoleBinding exists:

```sh
kubectl get clusterrolebinding | grep conduit
```

If it's missing, re-install the chart with `--set rbac.create=true`
(the default) — it was probably disabled in the previous run.

### App pods get connection refused on `conduit-agent.conduit.svc:4318`

NetworkPolicy is the usual cause. If your namespace has a default-
deny policy, allow egress to the conduit namespace:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-conduit-egress
  namespace: <your-app-namespace>
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: conduit
      ports:
        - protocol: TCP
          port: 4317
        - protocol: TCP
          port: 4318
```

## Next steps

- [**EKS-specific recipe**](../deploy/aws/eks.md) — IRSA for
  Honeycomb API key from Secrets Manager / SSM.
- [**Configuration reference**](../reference/configuration.md) — the
  full `conduit.yaml` schema; the chart's `values.yaml` is a thin
  layer over the same fields.
- [**Architecture overview**](../architecture/overview.md) — what
  every receiver / processor / exporter actually does.
- [**Troubleshooting index**](../troubleshooting/index.md) — every
  CDT code with fix steps.
