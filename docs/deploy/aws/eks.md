# Deploy Conduit on Amazon EKS

> **Audience**: a field engineer or platform engineer with an EKS cluster who wants Conduit running on every node, with the Honeycomb ingest key resolved through IRSA (no long-lived secrets in pod env). Goal: telemetry from every namespace landing in Honeycomb within 30 minutes.

## What you'll deploy

The Conduit Helm chart (M5.A — M5.D) on EKS as a DaemonSet. Each node runs one Conduit pod with:

- `hostmetrics` scraping `/hostfs` (the host bind mount the chart sets up).
- `kubeletstats` scraping the local node's kubelet.
- `filelog/k8s` tailing `/var/log/pods/*/*/*.log` with the upstream `container` operator.
- `k8sattributes` enriching every signal with the pod / namespace / deployment lookup the chart's RBAC grants read access to.
- An OTLP receiver bound to `0.0.0.0:4317` / `:4318` so peer pods can send to the node-local Conduit via the in-cluster service.

The Honeycomb ingest key is resolved via **IAM Roles for Service Accounts (IRSA)**: the Conduit pod's ServiceAccount is annotated with an IAM role ARN; pods get short-lived AWS credentials via the EKS pod identity webhook; the credentials are used to fetch the API key from SSM Parameter Store at pod startup.

```text
+--------- EKS node ---------+
|                            |
|  /hostfs (HostPath)        |
|  +------------+            |     OTLP/HTTPS
|  | Conduit    |--scrape--->|   +-------------+
|  | DaemonSet  |            |   |  Honeycomb  |
|  | pod        |            |---+>  api.hny.. |
|  +------------+            |   +-------------+
|     ^                      |
|     | OTLP from peer pods  |
|     | (in-cluster service) |
|     |                      |
|  +-------+  +--------+     |
|  | app A |  | app B  |     |
|  +-------+  +--------+     |
+----------------------------+
```

## Prerequisites

- An EKS cluster (1.28+ recommended for the OIDC-issuer changes, though 1.24+ also works).
- `kubectl` and `helm` (3.8+ — the chart is published as an OCI artifact, which requires Helm OCI support).
- `eksctl` (optional but documented; the same wiring is reachable via raw `aws iam` + `aws eks` calls).
- A Honeycomb ingest key (`hcaik_…`) either:
  - Stored in **AWS SSM Parameter Store** under `/conduit/<env>/honeycomb-api-key` (recommended; this recipe shows IRSA pulling from there), **or**
  - Set directly on a Kubernetes Secret you've already created (covered as a variation below).

## Step 1 — Enable the OIDC provider on the cluster

IRSA requires the cluster to have an OIDC provider registered with IAM. EKS clusters created with `eksctl create cluster --with-oidc` already have this; older clusters can add it with:

```bash
eksctl utils associate-iam-oidc-provider \
  --cluster my-cluster --approve
```

Verify:

```bash
aws eks describe-cluster --name my-cluster \
  --query 'cluster.identity.oidc.issuer' --output text
# https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B716D3041E
```

## Step 2 — Store the Honeycomb API key in Parameter Store

```bash
aws ssm put-parameter \
  --name '/conduit/prod/honeycomb-api-key' \
  --type SecureString \
  --value "$HONEYCOMB_API_KEY"
```

## Step 3 — Create the IAM role for Conduit's ServiceAccount

The role's trust policy delegates `sts:AssumeRoleWithWebIdentity` to the cluster's OIDC provider, scoped to **only** the `conduit-agent` ServiceAccount in the `conduit` namespace. The permissions policy grants `ssm:GetParameter` on the specific parameter ARN — nothing else.

The easiest path is `eksctl`:

```bash
eksctl create iamserviceaccount \
  --cluster my-cluster \
  --namespace conduit \
  --name conduit-agent \
  --role-name conduit-eks-irsa \
  --attach-policy-arn "$(aws iam create-policy \
    --policy-name conduit-eks-read-api-key \
    --policy-document '{
      "Version":"2012-10-17",
      "Statement":[{
        "Sid":"ReadHoneycombApiKey",
        "Effect":"Allow",
        "Action":"ssm:GetParameter",
        "Resource":"arn:aws:ssm:us-east-1:111122223333:parameter/conduit/prod/honeycomb-api-key"
      }]
    }' --query 'Policy.Arn' --output text)" \
  --approve --override-existing-serviceaccounts
```

This:

1. Creates the IAM policy.
2. Creates the IAM role with the right OIDC trust relationship.
3. Creates (or updates) the `conduit-agent` ServiceAccount in the `conduit` namespace with the `eks.amazonaws.com/role-arn` annotation.

The chart's default ServiceAccount name is `conduit-agent`; if you customize it via `serviceAccount.name` in values.yaml, pass that name to `eksctl create iamserviceaccount` instead.

## Step 4 — Install the Helm chart

The chart is OCI-published per [ADR-0019](../../adr/adr-0019.md):

```bash
helm install conduit-agent \
  oci://ghcr.io/conduit-obs/charts/conduit-agent \
  --version 0.0.1 \
  --namespace conduit \
  --create-namespace \
  -f values.yaml
```

`values.yaml`:

```yaml
serviceAccount:
  # eksctl created this in step 3; tell the chart not to recreate it.
  create: false
  name: conduit-agent

# pod-startup init container resolves the API key from SSM and writes
# it to a tmpfs volume the conduit container reads as ${env:HONEYCOMB_
# API_KEY}. Keeps the value off the pod spec and out of `kubectl
# describe` output.
extraInitContainers:
  - name: fetch-api-key
    image: amazon/aws-cli:2.13.0
    command:
      - sh
      - -c
      - |
        set -euo pipefail
        aws ssm get-parameter \
          --region "$AWS_REGION" \
          --name '/conduit/prod/honeycomb-api-key' \
          --with-decryption \
          --query 'Parameter.Value' --output text > /etc/conduit-secrets/honeycomb-api-key
    env:
      - name: AWS_REGION
        value: us-east-1
    volumeMounts:
      - name: conduit-secrets
        mountPath: /etc/conduit-secrets

extraVolumes:
  - name: conduit-secrets
    emptyDir:
      medium: Memory   # tmpfs, never hits node disk

extraVolumeMounts:
  - name: conduit-secrets
    mountPath: /etc/conduit-secrets
    readOnly: true

# The conduit container reads the env var via the entrypoint helper;
# alternatively, configure conduit.yaml to source from a file path
# (M10 ships --env-file as a first-class flag).
extraEnv:
  - name: HONEYCOMB_API_KEY_FILE
    value: /etc/conduit-secrets/honeycomb-api-key

# Cluster-name resource attribute — propagates to every signal so
# multi-cluster Honeycomb queries can scope to the right environment.
resourceAttributes:
  k8s.cluster.name: my-cluster
  deployment.environment: production
```

(`extraInitContainers` / `extraVolumes` / `extraVolumeMounts` / `extraEnv` are part of the chart's standard escape-hatch surface; see [`deploy/helm/conduit-agent/values.yaml`](../../../deploy/helm/conduit-agent/values.yaml) for the full list.)

The chart's defaults already set the M5 RBAC (read-only ClusterRole over pods/nodes/namespaces; documented inline in `templates/clusterrole.yaml`) and the DaemonSet host-mount story. No further configuration is required.

## Step 5 — Verify

```bash
# Pods running on every node
kubectl -n conduit get pods -l app.kubernetes.io/name=conduit-agent -o wide

# Health endpoint reachable from inside the pod
POD=$(kubectl -n conduit get pods -l app.kubernetes.io/name=conduit-agent -o jsonpath='{.items[0].metadata.name}')
kubectl -n conduit exec "$POD" -c conduit -- wget --quiet -O- http://127.0.0.1:13133/

# Recent collector logs
kubectl -n conduit logs "$POD" -c conduit --tail 100

# When `conduit doctor` lands (M11):
kubectl -n conduit exec "$POD" -c conduit -- conduit doctor --json
```

You should see:

- One Conduit pod per node, all `Running` / `Ready 1/1`.
- `:13133/` returning `200 OK`.
- Collector logs reporting `Everything is ready. Begin running and processing data.`.
- Within ~5 minutes, the [`dashboards/k8s-cluster-overview.json`](../../../dashboards/k8s-cluster-overview.json) board lights up: pod CPU / memory utilization, container uptime (the M5.E restart proxy), filesystem usage, log volume by namespace.

## Routing app traffic to Conduit

The chart provisions a headless Service `conduit-agent` in the `conduit` namespace, exposing OTLP on `:4317` (gRPC) and `:4318` (HTTP). Apps target the node-local pod via the `app.kubernetes.io/name` label or the cluster-wide service:

```yaml
# Per-pod, route to the Conduit on the same node — best for latency
# and to keep traffic node-local.
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://$(NODE_IP):4318
  - name: NODE_IP
    valueFrom:
      fieldRef:
        fieldPath: status.hostIP

# Or, cluster-wide service if your CNI gives every node a pod with the
# same hostNetwork:
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://conduit-agent.conduit.svc.cluster.local:4318
```

Both work. The `status.hostIP` pattern wins for traffic locality (each pod talks to the Conduit on its own node); the cluster service is simpler and works fine for low-volume workloads.

## Common variations

### Use an in-cluster Secret instead of SSM + IRSA

If your team already has a Vault or External Secrets Operator wiring telemetry-key secrets into the cluster, skip the IRSA dance:

```yaml
existingSecret: conduit-honeycomb     # Secret with key `api-key` in the
                                      # conduit namespace
existingSecretKey: api-key
```

…and the chart wires the value as `${env:HONEYCOMB_API_KEY}` directly. Drop the `extraInitContainers` / `extraVolumes` blocks above. The IAM role is then unnecessary — Conduit makes no AWS API calls.

### Run on Fargate-only EKS profiles

Fargate has no node concept, so a DaemonSet doesn't deploy onto it. Two options:

1. Use the **ECS sidecar pattern from [`ecs.md`](ecs.md)** instead — Fargate-on-EKS pods can run a Conduit sidecar via [the Helm chart's `sidecarMode: true` flag (V1)] or via a custom pod template that adds a Conduit container.
2. Run the DaemonSet on the EC2-backed node groups in your cluster and have the Fargate pods send to a cluster-wide service. This is the V0 default — the M5 chart targets node-bound pods.

### Honeycomb EU endpoint

```yaml
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}
    endpoint: https://api.eu1.honeycomb.io
```

(The `endpoint` knob lands in M10's output-mode work.)

### Air-gapped / VPC-restricted clusters

If the cluster can't reach `api.honeycomb.io` directly, point `output.mode: gateway` at a customer-operated OTLP gateway (M10), or use Honeycomb's PrivateLink endpoint (M10 docs once they land).

## See also

- [`docs/deploy/aws/README.md`](README.md) — common conventions across the four AWS recipes.
- [`deploy/helm/conduit-agent/README.md`](../../../deploy/helm/conduit-agent/README.md) — full values.yaml reference and the kind smoke-test path.
- [`internal/profiles/k8s/README.md`](../../../internal/profiles/k8s/README.md) — what `profile.mode: k8s` loads (kubelet, container logs, k8sattributes).
- [`dashboards/k8s-cluster-overview.json`](../../../dashboards/k8s-cluster-overview.json) — the M5.E board, ready to apply via `conduit board apply` (M11).
- [`docs/adr/adr-0019.md`](../../adr/adr-0019.md) — chart distribution decisions (OCI registry, cosign signing).
